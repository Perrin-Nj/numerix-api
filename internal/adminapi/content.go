package adminapi

// Question bank administration (docs/ADMIN_API.md, "Content"): CRUD with versions, filters, validation,
// the review lifecycle, import/export, world and quest edits, coverage and the AI draft.
//
// Go for Spring people: contentRoutes registers more mux patterns on the same router, like a second
// @RestController. Every mutation runs in one write transaction (contentdb.Repo.WithTx) that also writes the
// version snapshot, the audit rows and the Mongo outbox rows, then drops the question from the serving cache.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/seed"
)

// contentRoutes registers the content endpoints.
func (a *API) contentRoutes(mux *http.ServeMux) {
	const p = "/admin/v1"
	mux.HandleFunc("GET "+p+"/questions", a.listQuestions)
	mux.HandleFunc("POST "+p+"/questions", a.createQuestion)
	mux.HandleFunc("POST "+p+"/questions/validate", a.validateOne)
	mux.HandleFunc("POST "+p+"/questions/validate-all", a.validateAll)
	mux.HandleFunc("POST "+p+"/questions/ai-draft", a.aiDraft)
	mux.HandleFunc("GET "+p+"/questions/export", a.exportQuestions)
	mux.HandleFunc("POST "+p+"/questions/import", a.importQuestions)
	mux.HandleFunc("GET "+p+"/questions/{id}", a.getQuestion)
	mux.HandleFunc("PUT "+p+"/questions/{id}", a.putQuestion)
	mux.HandleFunc("POST "+p+"/questions/{id}/transition", a.transition)
	mux.HandleFunc("GET "+p+"/questions/{id}/versions", a.listVersions)
	mux.HandleFunc("GET "+p+"/questions/{id}/versions/{n}", a.getVersion)
	mux.HandleFunc("POST "+p+"/questions/{id}/restore", a.restoreVersion)
	mux.HandleFunc("GET "+p+"/worlds", a.listWorlds)
	mux.HandleFunc("PUT "+p+"/worlds/{id}", a.putWorld)
	mux.HandleFunc("GET "+p+"/quests", a.listQuests)
	mux.HandleFunc("PUT "+p+"/quests/{id}", a.putQuest)
	mux.HandleFunc("GET "+p+"/content/coverage", a.coverage)
	mux.HandleFunc("GET "+p+"/assets", a.listAssets)
	mux.HandleFunc("POST "+p+"/assets", a.createAsset)
	mux.HandleFunc("GET "+p+"/assets/search", a.searchAssets)
	mux.HandleFunc("GET "+p+"/assets/resolve", a.resolveAsset)
	mux.HandleFunc("GET "+p+"/assets/{id}", a.getAsset)
	mux.HandleFunc("PATCH "+p+"/assets/{id}", a.patchAsset)
}

// ---- shared helpers ----

var (
	graphOnce sync.Once
	graphVal  *engine.Graph
	graphErr  error
)

// graph loads the embedded curriculum once.
func loadGraph() (*engine.Graph, error) {
	graphOnce.Do(func() { graphVal, graphErr = engine.LoadGraph() })
	return graphVal, graphErr
}

// env builds the validator environment from the database (quests, misconception codes, assets, vocabulary).
func (a *API) env(ctx context.Context) (*content.Env, error) {
	g, err := loadGraph()
	if err != nil {
		return nil, fmt.Errorf("load curriculum: %w", err)
	}
	quests, err := a.content.Quests(ctx, a.db.R, "")
	if err != nil {
		return nil, err
	}
	byID := map[string]contentdb.QuestRow{}
	for _, q := range quests {
		byID[q.ID] = q
	}
	codes, err := a.content.MisconceptionCodes(ctx, a.db.R)
	if err != nil {
		return nil, err
	}
	vocab, err := a.assets.VocabSet(ctx, a.db.R)
	if err != nil {
		return nil, err
	}
	env := &content.Env{
		Skill: func(id string) (content.SkillInfo, bool) {
			sk, err := g.Get(id)
			if err != nil {
				return content.SkillInfo{}, false
			}
			return content.SkillInfo{YearIntro: sk.YearIntro}, true
		},
		Quest: func(id string) (content.QuestInfo, bool) {
			q, ok := byID[id]
			return content.QuestInfo{World: q.World, Skill: q.Skill, GradeMin: q.GradeMin, GradeMax: q.GradeMax, TargetCount: q.TargetCount}, ok
		},
		Misconception: func(c string) bool { return codes[c] },
		Asset: func(ref string) bool {
			var n int
			return a.db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM assets WHERE id = ? OR alias = ?`, ref, ref).Scan(&n) == nil && n > 0
		},
		Names: map[string]bool{},
	}
	for _, w := range seed.Denylist() {
		env.Denylist = append(env.Denylist, content.Fold(w))
	}
	for _, n := range seed.Names() {
		env.Names[content.Fold(n)] = true
	}
	if len(vocab) > 0 {
		env.Vocab = func(facet, v string) bool { return vocab[[2]string{facet, v}] }
	}
	return env, nil
}

// readBody reads the whole (already size-capped) request body.
func readBody(r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, httpapi.NewError(http.StatusRequestEntityTooLarge, "payload_too_large", "The request body is too large.")
	}
	return b, nil
}

// decodeQuestion reads a question object strictly. The error text names the problem, never echoes the body.
func decodeQuestion(raw []byte) (content.Question, error) {
	var q content.Question
	if err := content.DecodeStrict(raw, &q); err != nil {
		return q, httpapi.Validation(httpapi.FieldError{Field: "body", Message: "not a valid question object: " + err.Error()})
	}
	return q, nil
}

// issuesToError turns blocking issues into a 422 with one field error per issue.
func issuesToError(is []content.Issue) error {
	var fields []httpapi.FieldError
	for _, i := range content.Errors(is) {
		fields = append(fields, httpapi.FieldError{Field: i.Field, Message: i.Code + ": " + i.Message})
	}
	return httpapi.Validation(fields...)
}

// mapContentErr turns repository errors into API errors.
func mapContentErr(err error) error {
	var inv *contentdb.InvalidError
	switch {
	case errors.Is(err, contentdb.ErrNotFound):
		return httpapi.ErrNotFound
	case errors.Is(err, contentdb.ErrUnknownQuest), errors.Is(err, contentdb.ErrQuestMismatch):
		return httpapi.Validation(httpapi.FieldError{Field: "quest", Message: err.Error()})
	case errors.Is(err, contentdb.ErrVersionConflict):
		return httpapi.NewError(http.StatusConflict, "version_conflict", "Someone saved a newer version. Reload and merge.")
	case errors.Is(err, contentdb.ErrBadTransition):
		return httpapi.NewError(http.StatusConflict, "transition_not_allowed", err.Error())
	case errors.Is(err, contentdb.ErrNeedsReviewer):
		return httpapi.NewError(http.StatusConflict, "needs_reviewer", "An AI draft must be published by a reviewer other than its author.")
	case errors.As(err, &inv):
		return issuesToError(inv.Issues)
	}
	return err
}

// questionView is the admin representation of one question.
type questionView struct {
	ID          string                 `json:"id"`
	World       string                 `json:"world"`
	Quest       string                 `json:"quest"`
	Status      string                 `json:"status"`
	Version     int                    `json:"version"`
	ContentHash string                 `json:"content_hash"`
	AIDrafted   bool                   `json:"ai_drafted"`
	ReviewedBy  string                 `json:"reviewed_by,omitempty"`
	PublishedAt string                 `json:"published_at,omitempty"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
	Question    content.Question       `json:"question"`
	Versions    []contentdb.VersionRow `json:"versions,omitempty"`
	Issues      []content.Issue        `json:"issues"`
}

// view builds the response for a stored row, with its validation report.
func (a *API) view(ctx context.Context, row contentdb.Row, env *content.Env, withVersions bool) (questionView, error) {
	v := questionView{ID: row.Q.ID, World: row.WorldID, Quest: row.Q.Quest, Status: row.Q.Status, Version: row.Q.Version, ContentHash: row.Hash,
		AIDrafted: row.Q.AIDrafted, ReviewedBy: row.Q.ReviewedBy, PublishedAt: row.PublishedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Question: row.Q}
	v.Issues = env.Validate(&row.Q)
	if v.Issues == nil {
		v.Issues = []content.Issue{}
	}
	if withVersions {
		vers, err := a.content.Versions(ctx, a.db.R, row.Q.ID)
		if err != nil {
			return v, err
		}
		v.Versions = vers
	}
	return v, nil
}

// ifMatch reads the If-Match header ("3" or "\"3\"") as a version; 0 when absent.
func ifMatch(r *http.Request) (int, error) {
	h := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`)
	if h == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(h)
	if err != nil || n < 1 {
		return 0, bad("If-Match must be the version number you edited")
	}
	return n, nil
}

// checkStructure refuses what the database constraints would refuse anyway, with a readable message.
func checkStructure(q *content.Question) error {
	var fields []httpapi.FieldError
	add := func(f, m string) { fields = append(fields, httpapi.FieldError{Field: f, Message: m}) }
	if _, ok := content.AllowedKinds[q.Type]; !ok {
		add("type", "unknown type "+q.Type)
	}
	if q.Answer.Kind == "" {
		add("answer.kind", "required")
	}
	if q.Grade < 1 || q.Grade > 6 {
		add("grade", "1 to 6")
	}
	if q.Difficulty < 1 || q.Difficulty > 5 {
		add("difficulty", "1 to 5")
	}
	if q.Quest == "" {
		add("quest", "required")
	}
	if len(fields) > 0 {
		return httpapi.Validation(fields...)
	}
	return nil
}

// ---- list, read ----

// GET /questions
func (a *API) listQuestions(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	pg, size, _ := page(r)
	f := contentdb.Filter{World: qs.Get("world"), Quest: qs.Get("quest"), Skill: qs.Get("skill"), Type: qs.Get("type"), Status: qs.Get("status"),
		Author: qs.Get("author"), Tag: qs.Get("tag"), Text: qs.Get("q"), Page: pg, PageSize: size}
	f.Grade, _ = strconv.Atoi(qs.Get("grade"))
	f.Difficulty, _ = strconv.Atoi(qs.Get("difficulty"))
	if v := qs.Get("ai"); v != "" {
		on := v == "1" || v == "true"
		f.AI = &on
	}
	items, total, err := a.content.List(r.Context(), a.db.R, f)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf[contentdb.Summary]{Items: items, Page: pg, PageSize: size, Total: total})
}

// GET /questions/{id}
func (a *API) getQuestion(w http.ResponseWriter, r *http.Request) {
	row, err := a.content.Get(r.Context(), a.db.R, r.PathValue("id"))
	if err != nil {
		a.fail(w, r, mapContentErr(err))
		return
	}
	env, err := a.env(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v, err := a.view(r.Context(), row, env, true)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// GET /questions/{id}/versions
func (a *API) listVersions(w http.ResponseWriter, r *http.Request) {
	vers, err := a.content.Versions(r.Context(), a.db.R, r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if len(vers) == 0 {
		a.fail(w, r, httpapi.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": vers})
}

// GET /questions/{id}/versions/{n}
func (a *API) getVersion(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		a.fail(w, r, httpapi.ErrNotFound)
		return
	}
	q, err := a.content.Snapshot(r.Context(), a.db.R, r.PathValue("id"), n)
	if err != nil {
		a.fail(w, r, mapContentErr(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": n, "question": q})
}

// ---- create, edit ----

// nextID finds the next free id "<quest>.NNN" below 900 (900 and up are reserved for the sample questions).
func (a *API) nextID(ctx context.Context, tx *sql.Tx, quest string) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM bank_questions WHERE quest_id = ?`, quest)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	hi := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		if n, err := strconv.Atoi(id[strings.LastIndex(id, ".")+1:]); err == nil && n < 900 {
			hi = max(hi, n)
		}
	}
	if hi >= 899 {
		return "", bad("this quest already has 899 questions")
	}
	return fmt.Sprintf("%s.%03d", quest, hi+1), rows.Err()
}

// POST /questions: create a draft.
func (a *API) createQuestion(w http.ResponseWriter, r *http.Request) {
	raw, err := readBody(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	q, err := decodeQuestion(raw)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := checkStructure(&q); err != nil {
		a.fail(w, r, err)
		return
	}
	who := actor(r)
	q.Status, q.Version, q.Author, q.Source, q.AIDrafted, q.ReviewedBy = content.StatusDraft, 0, who, "admin", false, ""
	ctx := r.Context()
	env, err := a.env(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	err = a.content.WithTx(ctx, func(tx *sql.Tx) ([]string, error) {
		if q.ID == "" {
			if q.ID, err = a.nextID(ctx, tx, q.Quest); err != nil {
				return nil, err
			}
		} else if _, gerr := a.content.Get(ctx, tx, q.ID); gerr == nil {
			return nil, httpapi.NewError(http.StatusConflict, "already_exists", "A question with this id exists. Use PUT to change it.")
		}
		_, e := a.content.Save(ctx, tx, contentdb.SaveInput{Q: q, Actor: who, Action: "create"})
		return []string{q.ID}, e
	})
	if err != nil {
		a.fail(w, r, mapContentErr(err))
		return
	}
	saved, err := a.content.Get(ctx, a.db.R, q.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v, err := a.view(ctx, saved, env, false)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// PUT /questions/{id}: save a new version. The author, source and AI flag of the stored question are kept.
func (a *API) putQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	raw, err := readBody(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	in, err := decodeQuestion(raw)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	expect, err := ifMatch(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	who := actor(r)
	env, err := a.env(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	err = a.content.WithTx(ctx, func(tx *sql.Tx) ([]string, error) {
		cur, e := a.content.Get(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		if cur.Q.Status == content.StatusRetired {
			return nil, httpapi.NewError(http.StatusConflict, "transition_not_allowed", "A retired question cannot be edited. Restore it first.")
		}
		q := in
		q.ID, q.Author, q.Source, q.AIDrafted = id, cur.Q.Author, cur.Q.Source, cur.Q.AIDrafted
		q.Status, q.ReviewedBy = cur.Q.Status, ""
		if cur.Q.Status == content.StatusPublished { // an edited published question goes back to review
			q.Status = content.StatusReview
		}
		if e := checkStructure(&q); e != nil {
			return nil, e
		}
		_, e = a.content.Save(ctx, tx, contentdb.SaveInput{Q: q, Actor: who, Action: "edit", ExpectVersion: expect})
		return []string{id}, e
	})
	if err != nil {
		a.fail(w, r, mapContentErr(err))
		return
	}
	row, err := a.content.Get(ctx, a.db.R, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v, err := a.view(ctx, row, env, true)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// POST /questions/{id}/transition {to, comment}
func (a *API) transition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	var body struct {
		To      string `json:"to"`
		Comment string `json:"comment"`
	}
	if err := decode(r, &body); err != nil {
		a.fail(w, r, err)
		return
	}
	if len([]rune(body.Comment)) > 300 {
		a.fail(w, r, bad("comment is limited to 300 characters"))
		return
	}
	env, err := a.env(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	who := actor(r)
	err = a.content.WithTx(ctx, func(tx *sql.Tx) ([]string, error) {
		_, e := a.content.Transition(ctx, tx, id, body.To, who, body.Comment, env.Validate)
		return []string{id}, e
	})
	if err != nil {
		a.fail(w, r, mapContentErr(err))
		return
	}
	row, err := a.content.Get(ctx, a.db.R, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v, err := a.view(ctx, row, env, true)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// POST /questions/{id}/restore {version}: the old version becomes a new draft version.
func (a *API) restoreVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	var body struct {
		Version int `json:"version"`
	}
	if err := decode(r, &body); err != nil || body.Version < 1 {
		a.fail(w, r, bad("version is required"))
		return
	}
	who := actor(r)
	err := a.content.WithTx(ctx, func(tx *sql.Tx) ([]string, error) {
		_, e := a.content.Restore(ctx, tx, id, body.Version, who)
		return []string{id}, e
	})
	if err != nil {
		a.fail(w, r, mapContentErr(err))
		return
	}
	env, err := a.env(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	row, err := a.content.Get(ctx, a.db.R, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v, err := a.view(ctx, row, env, true)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// ---- validate ----

// POST /questions/validate: same rules as the seed and the publish step, for the editor's live panel.
func (a *API) validateOne(w http.ResponseWriter, r *http.Request) {
	raw, err := readBody(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	env, err := a.env(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	q, issues := a.validateBody(env, raw)
	if issues == nil {
		issues = []content.Issue{}
	}
	out := map[string]any{"ok": !content.HasErrors(issues), "issues": issues}
	if q != nil {
		var unverified bool
		var corrections []string
		cp := *q
		corrections, unverified = cp.ApplyCalc()
		out["unverified"], out["calc_corrections"] = unverified, corrections
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /questions/validate-all: validates every non-retired question (optionally ?world=).
func (a *API) validateAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	env, err := a.env(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	qs, err := a.content.Export(ctx, a.db.R, r.URL.Query().Get("world"), "")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	type entry struct {
		ID     string          `json:"id"`
		Issues []content.Issue `json:"issues"`
	}
	var failing []entry
	withErrors, withWarnings := 0, 0
	for i := range qs {
		is := env.Validate(&qs[i])
		if content.HasErrors(is) {
			withErrors++
		} else if len(is) > 0 {
			withWarnings++
		}
		if len(is) > 0 && len(failing) < 500 {
			failing = append(failing, entry{qs[i].ID, is})
		}
	}
	ptrs := make([]*content.Question, len(qs))
	for i := range qs {
		ptrs[i] = &qs[i]
	}
	set := env.ValidateSet(ptrs)
	if set == nil {
		set = []content.Issue{}
	}
	if failing == nil {
		failing = []entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(qs), "with_errors": withErrors, "with_warnings_only": withWarnings, "questions": failing, "set_issues": set})
}

// ---- export, import ----

// GET /questions/export?world=&status=
func (a *API) exportQuestions(w http.ResponseWriter, r *http.Request) {
	world, status := r.URL.Query().Get("world"), r.URL.Query().Get("status")
	qs, err := a.content.Export(r.Context(), a.db.R, world, status)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.auditRead(r.Context(), r, "questions.export", "question", orDefaultStr(world, "all"), fmt.Sprintf("%d questions", len(qs))); err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema": 1, "world": world, "exported_at": a.stamp(), "questions": qs})
}

func orDefaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// importReport is the answer of POST /questions/import.
type importReport struct {
	DryRun    bool          `json:"dry_run"`
	Added     int           `json:"added"`
	Changed   int           `json:"changed"`
	Unchanged int           `json:"unchanged"`
	Invalid   []invalidItem `json:"invalid"`
}

type invalidItem struct {
	ID     string          `json:"id"`
	Issues []content.Issue `json:"issues"`
}

var errDryRun = errors.New("dry run")

// POST /questions/import?dry_run=1&publish=1. Body: {"questions":[...]} (the export shape) or a bare array.
// New or changed questions are written as drafts; with publish=1 they keep the requested status only when they
// validate. An unchanged question is left alone whatever its status, so export then import is a no-op.
func (a *API) importQuestions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw, err := readBody(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	in, err := decodeQuestions(raw)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if len(in) > 2000 {
		a.fail(w, r, bad("at most 2000 questions per import"))
		return
	}
	dry := r.URL.Query().Get("dry_run") == "1" || r.URL.Query().Get("dry_run") == "true"
	publish := r.URL.Query().Get("publish") == "1" || r.URL.Query().Get("publish") == "true"
	env, err := a.env(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	who := actor(r)
	rep := importReport{DryRun: dry, Invalid: []invalidItem{}}
	var ids []string
	err = a.db.WithWriteTx(ctx, func(tx *sql.Tx) error {
		for i := range in {
			q := in[i]
			if q.ID == "" {
				rep.Invalid = append(rep.Invalid, invalidItem{ID: fmt.Sprintf("#%d", i), Issues: []content.Issue{{Code: "id_format", Field: "id", Message: "id is required in an import"}}})
				continue
			}
			if e := checkStructure(&q); e != nil {
				rep.Invalid = append(rep.Invalid, invalidItem{ID: q.ID, Issues: []content.Issue{{Code: "structure", Field: "$", Message: e.Error()}}})
				continue
			}
			issues := env.Validate(&q)
			want := q.Status
			q.Status = content.StatusDraft
			if publish && !content.HasErrors(issues) && (want == content.StatusReview || want == content.StatusPublished) && !q.AIDrafted {
				q.Status = want
			}
			if q.Author == "" {
				q.Author = who
			}
			if q.Source == "" {
				q.Source = "import:" + who
			}
			q.ReviewedBy = ""
			if q.Status == content.StatusPublished {
				q.ReviewedBy = who
			}
			res, e := a.content.Save(ctx, tx, contentdb.SaveInput{Q: q, Actor: who, Action: "import", KeepStatus: true, Detail: "import"})
			if e != nil {
				rep.Invalid = append(rep.Invalid, invalidItem{ID: q.ID, Issues: []content.Issue{{Code: "save", Field: "$", Message: e.Error()}}})
				continue
			}
			switch {
			case res.Created:
				rep.Added++
			case res.Changed:
				rep.Changed++
			default:
				rep.Unchanged++
			}
			if res.Changed {
				ids = append(ids, q.ID)
			}
		}
		if dry {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		a.fail(w, r, mapContentErr(err))
		return
	}
	if !dry {
		a.content.AfterCommit(ids...)
	}
	writeJSON(w, http.StatusOK, rep)
}

// ---- worlds, quests, coverage ----

// GET /worlds
func (a *API) listWorlds(w http.ResponseWriter, r *http.Request) {
	rows, err := a.content.Worlds(r.Context(), a.db.R)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

// GET /quests?world=
func (a *API) listQuests(w http.ResponseWriter, r *http.Request) {
	rows, err := a.content.Quests(r.Context(), a.db.R, r.URL.Query().Get("world"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

// PUT /worlds/{id}: edit names, hook, colour, grades, hero (the seed files stay the long-term owner).
func (a *API) putWorld(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	var in content.World
	if err := decode(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	cur, err := a.content.Worlds(ctx, a.db.R)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var old *contentdb.WorldRow
	for i := range cur {
		if cur[i].ID == id {
			old = &cur[i]
		}
	}
	if old == nil {
		a.fail(w, r, httpapi.ErrNotFound)
		return
	}
	merged := old.World
	if !in.Name.IsZero() {
		merged.Name = in.Name
	}
	if !in.Tagline.IsZero() {
		merged.Tagline = in.Tagline
	}
	if !in.Hook.IsZero() {
		merged.Hook = in.Hook
	}
	if in.AccentColor != "" {
		merged.AccentColor = in.AccentColor
	}
	if in.GradeMin != 0 {
		merged.GradeMin = in.GradeMin
	}
	if in.GradeMax != 0 {
		merged.GradeMax = in.GradeMax
	}
	if in.HeroAsset != "" {
		merged.HeroAsset = in.HeroAsset
	}
	if in.Status != "" {
		merged.Status = in.Status
	}
	if merged.Name.FR == "" || merged.Name.EN == "" || merged.Hook.FR == "" || merged.Hook.EN == "" || merged.GradeMin < 1 || merged.GradeMax > 6 || merged.GradeMin > merged.GradeMax ||
		len(merged.AccentColor) != 7 || merged.AccentColor[0] != '#' || (merged.Status != "draft" && merged.Status != "published" && merged.Status != "retired") {
		a.fail(w, r, bad("world needs fr and en name and hook, a #RRGGBB colour, grades 1 to 6 and a valid status"))
		return
	}
	who := actor(r)
	err = a.db.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := a.content.UpsertWorld(ctx, tx, merged, who)
		return e
	})
	if err != nil {
		a.fail(w, r, bad(err.Error()))
		return
	}
	a.content.AfterCommit() // worlds changed: drop the cached world lists
	cur, _ = a.content.Worlds(ctx, a.db.R)
	for _, c := range cur {
		if c.ID == id {
			writeJSON(w, http.StatusOK, c)
			return
		}
	}
	writeJSON(w, http.StatusOK, merged)
}

// PUT /quests/{id}: edit names, goal, target count, grades, icon.
func (a *API) putQuest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	var in content.Quest
	if err := decode(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	all, err := a.content.Quests(ctx, a.db.R, "")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var old *contentdb.QuestRow
	for i := range all {
		if all[i].ID == id {
			old = &all[i]
		}
	}
	if old == nil {
		a.fail(w, r, httpapi.ErrNotFound)
		return
	}
	merged := old.Quest
	if !in.Name.IsZero() {
		merged.Name = in.Name
	}
	if !in.Goal.IsZero() {
		merged.Goal = in.Goal
	}
	if in.TargetCount != 0 {
		merged.TargetCount = in.TargetCount
	}
	if in.GradeMin != 0 {
		merged.GradeMin = in.GradeMin
	}
	if in.GradeMax != 0 {
		merged.GradeMax = in.GradeMax
	}
	if in.IconAsset != "" {
		merged.IconAsset = in.IconAsset
	}
	if in.Status != "" {
		merged.Status = in.Status
	}
	if merged.Name.FR == "" || merged.Name.EN == "" || merged.TargetCount < 1 || merged.GradeMin < 1 || merged.GradeMax > 6 || merged.GradeMin > merged.GradeMax ||
		(merged.Status != "draft" && merged.Status != "published" && merged.Status != "retired") {
		a.fail(w, r, bad("quest needs fr and en name, target_count >= 1, grades 1 to 6 and a valid status"))
		return
	}
	who := actor(r)
	if err := a.db.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := a.content.UpsertQuest(ctx, tx, merged, who)
		return e
	}); err != nil {
		a.fail(w, r, bad(err.Error()))
		return
	}
	a.content.AfterCommit()
	writeJSON(w, http.StatusOK, merged)
}

// GET /content/coverage: per quest counts against the target, and per world the type counts against the quota.
func (a *API) coverage(w http.ResponseWriter, r *http.Request) {
	cov, err := a.content.Coverage(r.Context(), a.db.R)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	type worldCov struct {
		World   string         `json:"world"`
		ByType  map[string]int `json:"by_type"`
		Missing map[string]int `json:"quota_missing"`
	}
	quota := map[string]int{content.TypeNumeric: 10, content.TypeChoice: 10, content.TypeWord: 8, content.TypeTrueFalse: 5, content.TypeOrder: 5, content.TypeMatch: 5, content.TypeVisual: 5, content.TypeEstimate: 4}
	byWorld := map[string]*worldCov{}
	var order []string
	for _, c := range cov {
		wc := byWorld[c.World]
		if wc == nil {
			wc = &worldCov{World: c.World, ByType: map[string]int{}, Missing: map[string]int{}}
			byWorld[c.World] = wc
			order = append(order, c.World)
		}
		for t, n := range c.ByType {
			wc.ByType[t] += n
		}
	}
	worlds := make([]worldCov, 0, len(order))
	for _, id := range order {
		wc := byWorld[id]
		for t, q := range quota {
			if d := q - wc.ByType[t]; d > 0 {
				wc.Missing[t] = d
			}
		}
		worlds = append(worlds, *wc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"quests": cov, "worlds": worlds})
}

// ---- AI draft ----

// draftLimiter allows 30 drafts per hour per admin (in memory; one process runs the API).
type draftLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

const draftsPerHour = 30

// allow records a draft attempt and reports whether it is within the limit, plus the seconds to wait when not.
func (l *draftLimiter) allow(who string, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	cut := now.Add(-time.Hour)
	var keep []time.Time
	for _, t := range l.hits[who] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= draftsPerHour {
		l.hits[who] = keep
		return false, int(keep[0].Add(time.Hour).Sub(now).Seconds()) + 1
	}
	l.hits[who] = append(keep, now)
	return true, 0
}

// decodeQuestions reads {"questions":[...]} (the export and seed-file shape) or a bare array, each question strictly.
func decodeQuestions(raw []byte) ([]content.Question, error) {
	var items []json.RawMessage
	var wrapped struct {
		Questions []json.RawMessage `json:"questions"`
	}
	trimmed := strings.TrimSpace(string(raw))
	switch {
	case strings.HasPrefix(trimmed, "["):
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, httpapi.Validation(httpapi.FieldError{Field: "body", Message: "not a JSON array"})
		}
	case strings.HasPrefix(trimmed, "{"):
		if err := json.Unmarshal(raw, &wrapped); err != nil || wrapped.Questions == nil {
			return nil, httpapi.Validation(httpapi.FieldError{Field: "body", Message: "expected a \"questions\" array"})
		}
		items = wrapped.Questions
	default:
		return nil, httpapi.Validation(httpapi.FieldError{Field: "body", Message: "expected {\"questions\":[...]} or an array of questions"})
	}
	out := make([]content.Question, 0, len(items))
	for i, it := range items {
		var q content.Question
		if err := content.DecodeStrict(it, &q); err != nil {
			return nil, httpapi.Validation(httpapi.FieldError{Field: fmt.Sprintf("questions[%d]", i), Message: err.Error()})
		}
		out = append(out, q)
	}
	return out, nil
}

// validateBody validates a question that may not have an id yet (the editor validates before the first save):
// a missing id gets a well-formed placeholder so only real problems are reported.
func (a *API) validateBody(env *content.Env, raw []byte) (*content.Question, []content.Issue) {
	var q content.Question
	if err := content.DecodeStrict(raw, &q); err != nil {
		return nil, []content.Issue{{Code: "json", Field: "$", Message: err.Error()}}
	}
	if q.ID == "" && q.Quest != "" {
		q.ID = q.Quest + ".000"
	}
	return &q, env.Validate(&q)
}
