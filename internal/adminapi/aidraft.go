package adminapi

// POST /admin/v1/questions/ai-draft: ask a language model for a DRAFT, then let the server check it.
//
// What the server guarantees (docs/CONTENT_PLAN.md section 8):
//   - without ANTHROPIC_API_KEY and NARRATOR_MODEL the endpoint answers 503 not_configured and nothing is called;
//   - the saved row is always status=draft, ai_drafted=1, author "ai:<model>", source "ai-draft";
//   - the model never supplies a final answer unverified: when `calc` is present the server recomputes it and
//     overwrites the stored number; the full validator runs and its report is returned (and stored in the audit
//     detail as a count), but an invalid draft is still saved so a human can fix it;
//   - publishing needs another admin than the author, enforced here and by a SQL CHECK;
//   - 30 drafts per hour per admin; the prompt and the key never reach the audit log.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"slices"

	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/drafter"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// draftRequest is the body of the endpoint.
type draftRequest struct {
	World      string `json:"world"` // optional, taken from the quest
	Quest      string `json:"quest"`
	Type       string `json:"type"`
	Grade      int    `json:"grade"`
	Difficulty int    `json:"difficulty"`
	TopicHint  string `json:"topic_hint"`
	Language   string `json:"language_primary"`
}

// kindForType is the default answer kind of a type.
var kindForType = map[string]string{
	content.TypeNumeric: content.KindNumeric, content.TypeChoice: content.KindChoice, content.TypeTrueFalse: content.KindTrueFalse,
	content.TypeOrder: content.KindOrder, content.TypeMatch: content.KindMatch, content.TypeWord: content.KindNumeric,
	content.TypeVisual: content.KindNumeric, content.TypeEstimate: content.KindRange,
}

func (a *API) aiDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if a.drafter == nil {
		a.fail(w, r, httpapi.NewError(http.StatusServiceUnavailable, "not_configured", "AI drafts need ANTHROPIC_API_KEY and NARRATOR_MODEL."))
		return
	}
	var in draftRequest
	if err := decode(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	kind, ok := kindForType[in.Type]
	if !ok {
		a.fail(w, r, bad("type must be one of numeric, choice, truefalse, order, match, word, visual, estimate"))
		return
	}
	if in.Language != "fr" && in.Language != "en" {
		in.Language = "fr"
	}
	if len([]rune(in.TopicHint)) > 200 {
		a.fail(w, r, bad("topic_hint is limited to 200 characters"))
		return
	}
	who := actor(r)
	quests, err := a.content.Quests(ctx, a.db.R, "")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	idx := slices.IndexFunc(quests, func(q contentdb.QuestRow) bool { return q.ID == in.Quest })
	if idx < 0 {
		a.fail(w, r, httpapi.Validation(httpapi.FieldError{Field: "quest", Message: "unknown quest"}))
		return
	}
	quest := quests[idx]
	if in.Grade == 0 {
		in.Grade = quest.GradeMin
	}
	if in.Difficulty == 0 {
		in.Difficulty = 3
	}
	if in.Grade < quest.GradeMin || in.Grade > quest.GradeMax || in.Difficulty < 1 || in.Difficulty > 5 {
		a.fail(w, r, bad("grade must sit inside the quest grades and difficulty must be 1 to 5"))
		return
	}
	if ok, wait := a.drafts.allow(who, a.now()); !ok {
		a.fail(w, r, httpapi.RateLimited(wait))
		return
	}
	g, err := loadGraph()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	skill, err := g.Get(quest.Skill)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	hints, err := a.misconceptionHints(ctx, quest.Skill)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	raw, err := a.drafter.Draft(ctx, drafter.Request{World: quest.World, Quest: quest.ID, QuestGoal: quest.Goal.FR, Skill: quest.Skill, SkillLabel: skill.Label,
		Type: in.Type, Kind: kind, Grade: in.Grade, Difficulty: in.Difficulty, TopicHint: in.TopicHint, Primary: in.Language, Misconceptions: hints})
	if err != nil {
		a.log.Warn("ai draft failed", "err", err) // no prompt, no key
		a.fail(w, r, httpapi.NewError(http.StatusBadGateway, "draft_failed", "The model did not return a usable draft. Try again."))
		return
	}
	var q content.Question
	if err := content.DecodeStrict(raw, &q); err != nil {
		a.log.Warn("ai draft unreadable", "err", err)
		a.fail(w, r, httpapi.NewError(http.StatusUnprocessableEntity, "draft_unreadable", "The model's draft did not follow the question format: "+err.Error()))
		return
	}
	model := a.drafter.Model()
	// the server decides everything the model must not decide
	q.Quest, q.Skill, q.Type, q.Grade, q.Difficulty = quest.ID, quest.Skill, in.Type, in.Grade, in.Difficulty
	q.Answer.Kind = kind
	if in.Type == content.TypeChoice || in.Type == content.TypeEstimate && len(q.Choices) > 0 {
		q.Answer.Kind = content.KindChoice
	}
	q.Status, q.Version, q.Author, q.Source, q.AIDrafted, q.ReviewedBy = content.StatusDraft, 0, "ai:"+model, "ai-draft", true, ""
	corrections, unverified := q.ApplyCalc()
	env, err := a.env(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	err = a.content.WithTx(ctx, func(tx *sql.Tx) ([]string, error) {
		var e error
		if q.ID, e = a.nextID(ctx, tx, quest.ID); e != nil {
			return nil, e
		}
		detail := fmt.Sprintf("ai draft by %s for %s (requested by %s)", model, who, who)
		_, e = a.content.Save(ctx, tx, contentdb.SaveInput{Q: q, Actor: who, Action: "ai_draft", Detail: detail})
		return []string{q.ID}, e
	})
	if err != nil {
		a.fail(w, r, mapContentErr(err))
		return
	}
	row, err := a.content.Get(ctx, a.db.R, q.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v, err := a.view(ctx, row, env, false)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if corrections == nil {
		corrections = []string{}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"question": v, "unverified": unverified, "calc_corrections": corrections,
		"needs_review": true, "valid": !content.HasErrors(v.Issues)})
}

// misconceptionHints lists the codes the model may use: the codes of the skill and the generic mis_* codes.
func (a *API) misconceptionHints(ctx context.Context, skill string) ([]drafter.Hint, error) {
	rows, err := a.db.R.QueryContext(ctx, `SELECT code, desc_fr FROM misconceptions WHERE skill_id = ? OR source = 'seed' ORDER BY (skill_id = ?) DESC, code LIMIT 80`, skill, skill)
	if err != nil {
		return nil, fmt.Errorf("list misconception hints: %w", err)
	}
	defer rows.Close()
	var out []drafter.Hint
	for rows.Next() {
		var h drafter.Hint
		if err := rows.Scan(&h.Code, &h.Desc); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
