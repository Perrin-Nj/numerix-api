package app

// Curated question bank in the game: pick, project, render and grade questions that come from the bank
// (docs/CONTENT_PLAN.md section 1.3). Generated questions keep their old path in game_*.go; everything that is
// specific to curated rows lives here.
//
// Go for Spring people: a curated row is stored twice on purpose. bank_questions is the editable source of
// truth (versioned, reviewed). `questions` is the table of SERVED instances that beats point to; the first time a
// child needs a bank question we copy ("project") its current version there, so editing the bank later never
// rewrites a question a child is looking at.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// bankRefresh is how long the skill graph trusts its picture of the bank before asking the database again
// (a seed run in another process, or an admin edit, shows up within this time; edits in this process are immediate).
const bankRefresh = 30 * time.Second

// contentState holds the small caches of the content features. Like gameState it hangs off the Server lazily.
type contentState struct {
	mu       sync.Mutex
	bankAt   time.Time
	bankGen  uint64
	worldsAt map[string]time.Time // per language
	worldsG  map[string]uint64
	worlds   map[string][]api.World
}

var contentStates sync.Map

// cs returns the Server's contentState.
func (s *Server) cs() *contentState {
	if v, ok := contentStates.Load(s); ok {
		return v.(*contentState)
	}
	v, _ := contentStates.LoadOrStore(s, &contentState{worldsAt: map[string]time.Time{}, worldsG: map[string]uint64{}, worlds: map[string][]api.World{}})
	return v.(*contentState)
}

// syncBank refreshes the skill graph's view of the bank when it is stale or content changed in this process.
// A failure is logged and the old view stays (the game keeps working on the generators).
func (s *Server) syncBank(ctx context.Context) {
	st := s.cs()
	st.mu.Lock()
	defer st.mu.Unlock()
	gen := s.Content.Cache.Generation()
	if !st.bankAt.IsZero() && time.Since(st.bankAt) < bankRefresh && gen == st.bankGen {
		return
	}
	counts, err := s.Content.BankCounts(ctx, s.Store.R)
	st.bankAt, st.bankGen = time.Now(), gen // also on failure, so a broken table is not queried on every request
	if err != nil {
		s.Log.Warn("refresh question bank view failed", "err", err)
		return
	}
	entries := make([]engine.BankEntry, len(counts))
	for i, c := range counts {
		entries[i] = engine.BankEntry{World: c.World, Skill: c.Skill, Published: c.Published}
	}
	s.game().graph.SetBank(entries)
}

// worldExists tells whether a world id is in the catalogue.
func (s *Server) worldExists(ctx context.Context, id string) (bool, error) {
	var n int
	if err := s.Store.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM worlds WHERE id = ? AND status <> 'retired'`, id).Scan(&n); err != nil {
		return false, s.plInternalErr(ctx, "check world", err)
	}
	return n > 0, nil
}

// toAPIMedia converts the store's MediaRef to the generated type (same JSON shape).
func toAPIMedia(m assetstore.MediaRef) (api.MediaRef, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return api.MediaRef{}, err
	}
	var out api.MediaRef
	return out, json.Unmarshal(b, &out)
}

// largestURL returns the URL of the widest variant of a MediaRef ("" when it has none).
func largestURL(m assetstore.MediaRef) string {
	if len(m.SrcSet) == 0 {
		return ""
	}
	return m.SrcSet[len(m.SrcSet)-1].URL
}

// worldViews builds the world list for a language from the database, with hero and scenes. The result is cached
// for bankRefresh and until content changes.
func (s *Server) worldViews(ctx context.Context, lang string) ([]api.World, error) {
	st := s.cs()
	st.mu.Lock()
	gen := s.Content.Cache.Generation()
	if cached, ok := st.worlds[lang]; ok && time.Since(st.worldsAt[lang]) < bankRefresh && st.worldsG[lang] == gen {
		st.mu.Unlock()
		return cached, nil
	}
	st.mu.Unlock()
	s.syncBank(ctx)
	rows, err := s.Content.Worlds(ctx, s.Store.R)
	if err != nil {
		return nil, s.plInternalErr(ctx, "list worlds", err)
	}
	base := s.Config.AssetBaseURL
	if base == "" {
		base = "/assets"
	}
	items := make([]api.World, 0, len(rows))
	for _, w := range rows {
		name, tag, hook := w.Name.Get(lang), w.Tagline.Get(lang), w.Hook.Get(lang)
		gmin, gmax := w.GradeMin, w.GradeMax
		item := api.World{
			Id: api.WorldId(w.ID), Name: name, Tagline: tag, Hook: &hook, ImageUrl: "", AccentColor: w.AccentColor,
			SkillCount: s.game().graph.WorldSkillCount(w.ID), GradeMin: &gmin, GradeMax: &gmax,
		}
		scenes := []api.MediaRef{}
		if hero, ok := s.Assets.WorldHero(ctx, s.Store.R, w.ID); ok {
			ref := hero.Ref(lang, base)
			if m, err := toAPIMedia(ref); err == nil {
				item.Hero, item.ImageUrl = &m, largestURL(ref)
			} else {
				s.Log.Warn("world hero skipped", "world", w.ID, "err", err)
			}
		}
		for _, a := range s.Assets.SceneAssets(ctx, s.Store.R, w.ID, 4) {
			if m, err := toAPIMedia(a.Ref(lang, base)); err == nil {
				scenes = append(scenes, m)
			}
		}
		item.Scenes = &scenes
		items = append(items, item)
	}
	st.mu.Lock()
	st.worlds[lang], st.worldsAt[lang], st.worldsG[lang] = items, time.Now(), gen
	st.mu.Unlock()
	return items, nil
}

// answerFromJSON turns JSON into the contract's AnswerValue.
func answerFromJSON(raw json.RawMessage) api.AnswerValue {
	var out api.AnswerValue
	_ = out.UnmarshalJSON(raw) // raw comes from content.CorrectAnswer: valid JSON by construction
	return out
}

// misconceptionDesc returns the French description of an error code for the mastery profile ("" when unknown).
func (s *Server) misconceptionDesc(ctx context.Context, q rowQuerier, code string) string {
	var d string
	if err := q.QueryRowContext(ctx, `SELECT desc_fr FROM misconceptions WHERE code = ?`, code).Scan(&d); err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.Log.Warn("read misconception description failed", "code", code, "err", err)
	}
	return d
}

// curatedOf returns the parsed bank question stored in a served problem, or nil error and nil question never:
// a problem without a curated payload is a programming error here. It uses the LRU keyed by id and version.
func (s *Server) curatedOf(p *engine.Problem) (*content.Question, error) {
	id, _ := p.Metadonnees["bank_id"].(string)
	ver := 0
	switch v := p.Metadonnees["bank_version"].(type) {
	case float64:
		ver = int(v)
	case int:
		ver = v
	}
	if q, ok := s.Content.Cache.Get(id, ver); ok && id != "" {
		return q, nil
	}
	var q content.Question
	if err := json.Unmarshal(p.Curated, &q); err != nil {
		return nil, fmt.Errorf("decode curated question %s: %w", id, err)
	}
	if id != "" {
		s.Content.Cache.Put(id, ver, &q)
	}
	return &q, nil
}

// curatedProblem builds the served-instance JSON of a bank question version.
func curatedProblem(q *content.Question, version int) (engine.Problem, error) {
	raw, err := json.Marshal(q)
	if err != nil {
		return engine.Problem{}, fmt.Errorf("encode curated question %s: %w", q.ID, err)
	}
	statement := q.MathText
	if statement == "" {
		statement = q.Prompt.FR
	}
	return engine.Problem{
		CompetenceID: q.Skill, Generateur: "curated", EnonceMath: statement, Reponse: string(q.CorrectAnswer("fr")),
		ContexteNarratif: map[string]any{}, Difficulte: float64(q.Difficulty) / 5,
		Metadonnees: map[string]any{"curated": true, "bank_id": q.ID, "bank_version": version, "world": strings.SplitN(q.ID, ".", 2)[0]},
		Shape:       q.Type, Curated: raw,
	}, nil
}

// pickCurated takes the best published bank question this child has not seen for (world, skill), copies the
// current version into `questions` and returns its served id. ok is false when the child exhausted the bank.
func (s *Server) pickCurated(ctx context.Context, tx *sql.Tx, childID, world, skill string, grade int, avoidShape string) (id, shape string, ok bool, err error) {
	cands, err := s.Content.Candidates(ctx, tx, childID, world, skill, grade)
	if err != nil || len(cands) == 0 {
		return "", "", false, err
	}
	prof, err := s.profileFrom(ctx, tx, childID)
	if err != nil {
		return "", "", false, err
	}
	best, found := contentdb.Rank(cands, childID, avoidShape, contentdb.TargetDifficulty(prof.Mastery(skill, s.game().now())), grade)
	if !found {
		return "", "", false, nil
	}
	q, err := s.Content.LoadVersion(ctx, tx, best.ID, best.Version)
	if err != nil {
		return "", "", false, err
	}
	p, err := curatedProblem(q, best.Version)
	if err != nil {
		return "", "", false, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", "", false, err
	}
	hash := fmt.Sprintf("bank:%s:v%d", best.ID, best.Version)
	if _, err = tx.ExecContext(ctx, `INSERT INTO questions (id, params_hash, skill_id, json, shape, bank_id, bank_version, origin) VALUES (?, ?, ?, ?, ?, ?, ?, 'curated')
		ON CONFLICT(params_hash) DO NOTHING`, newID(), hash, skill, string(raw), q.Type, best.ID, best.Version); err != nil {
		return "", "", false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT id, shape FROM questions WHERE params_hash = ?`, hash).Scan(&id, &shape); err != nil {
		return "", "", false, err
	}
	return id, shape, true, nil
}

// shuffled returns a copy of list in an order that depends only on the seed text, so a beat always shows the
// same arrangement (a reload must not reshuffle the cards).
func shuffled[T any](list []T, seed string, key func(T) string) []T {
	out := slices.Clone(list)
	weight := func(t T) uint64 {
		sum := sha256.Sum256([]byte(seed + "|" + key(t)))
		return binary.BigEndian.Uint64(sum[:8])
	}
	slices.SortFunc(out, func(a, b T) int {
		wa, wb := weight(a), weight(b)
		switch {
		case wa < wb:
			return -1
		case wa > wb:
			return 1
		}
		return 0
	})
	return out
}

// curatedBeatView renders a Beat for a bank question: the statement, the options and the pictures, never the
// answer, the explanation, the calc, the common errors or any misconception code (BE-67).
func (s *Server) curatedBeatView(ctx context.Context, b beatRow, cq *content.Question, lang string) (api.Beat, error) {
	var childID string
	if err := s.Store.R.QueryRowContext(ctx, `SELECT child_id FROM sessions WHERE id = ?`, b.SessionID).Scan(&childID); err != nil {
		return api.Beat{}, s.plInternalErr(ctx, "load beat owner", err)
	}
	kind := api.QuestionKind(cq.Type)
	prompt, story := cq.Prompt.Get(lang), cq.Story.Get(lang)
	math := cq.MathText
	if math == "" {
		math = prompt
	}
	hint := !cq.Hint.IsZero()
	view := api.Question{Kind: &kind, MathText: math, Choices: []api.AnswerValue{}, Prompt: &prompt, Story: &story, HintAvailable: &hint}
	if cq.Answer.Kind == content.KindChoice {
		for _, c := range shuffled(cq.Choices, b.ID, func(c content.Choice) string { return c.ID }) {
			view.Choices = append(view.Choices, answerFromJSON(content.ChoiceValue(c, lang)))
		}
	}
	if cq.Items != nil {
		item := func(it content.Item) api.QuestionItem { return api.QuestionItem{Id: it.ID, Text: it.Text.Get(lang)} } // the sort key is not sent
		items := api.QuestionItems{}
		conv := func(list []content.Item, shuffle bool) *[]api.QuestionItem {
			if len(list) == 0 {
				return nil
			}
			if shuffle {
				list = shuffled(list, b.ID, func(i content.Item) string { return i.ID })
			}
			out := make([]api.QuestionItem, len(list))
			for i, it := range list {
				out[i] = item(it)
			}
			return &out
		}
		if cq.Answer.Kind == content.KindOrder {
			shown := shuffled(cq.Items.List, b.ID, func(i content.Item) string { return i.ID })
			if ids := itemIDs(shown); slices.Equal(ids, cq.Answer.Sequence) && len(shown) > 1 { // never show the answer order
				shown = append(shown[1:], shown[0])
			}
			items.Cards = conv(shown, false)
		} else {
			items.Left, items.Right = conv(cq.Items.Left, false), conv(cq.Items.Right, true)
		}
		view.Items = &items
	}
	if cq.Visual != nil {
		v := api.QuestionVisual{Kind: api.QuestionVisualKind(cq.Visual.Kind)}
		if cq.Visual.Data != nil {
			data := cq.Visual.Data
			v.Data = &data
		}
		view.Visual = &v
	}
	base := s.Config.AssetBaseURL
	if base == "" {
		base = "/assets"
	}
	var scene *string
	media := []api.QuestionMedia{}
	for _, m := range s.Assets.MediaFor(ctx, s.Store.R, cq, childID, lang, base) {
		img, err := toAPIMedia(m.MediaRef)
		if err != nil {
			s.Log.Warn("question media skipped", "question", cq.ID, "err", err)
			continue
		}
		media = append(media, api.QuestionMedia{Role: m.Role, Image: img})
		if scene == nil && m.Role == "scene" {
			if u := largestURL(m.MediaRef); u != "" {
				scene = &u
			}
		}
	}
	view.Media = &media
	created := plParseTS(b.Created)
	return api.Beat{
		Id: plUUID(b.ID), SessionId: plUUID(b.SessionID), Index: b.Index, SkillId: cq.Skill, Question: view,
		Scene:     api.Narration{Status: api.Ready, Text: &story, IllustrationUrl: scene},
		CreatedAt: &created,
	}, nil
}

// itemIDs lists the ids of items.
func itemIDs(list []content.Item) []string {
	out := make([]string, len(list))
	for i, it := range list {
		out[i] = it.ID
	}
	return out
}

// RequestHint answers POST /sessions/{sessionId}/beats/{beatId}/hint.
func (s *Server) RequestHint(ctx context.Context, req api.RequestHintRequestObject) (api.RequestHintResponseObject, error) {
	r, c, err := s.loadSession(ctx, plUID(req.SessionId))
	if err != nil {
		return nil, err
	}
	lang := gameLang(req.Params.AcceptLanguage, c.Locale)
	var qid string
	var answered int
	err = s.Store.R.QueryRowContext(ctx, `SELECT b.question_id, (SELECT COUNT(*) FROM answers a WHERE a.beat_id = b.id) FROM beats b WHERE b.id = ? AND b.session_id = ?`,
		plUID(req.BeatId), r.ID).Scan(&qid, &answered)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpapi.ErrNotFound
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "load beat", err)
	}
	if answered > 0 {
		return nil, plConflict("beat_answered", "This question was already answered.")
	}
	q, err := loadQuestion(ctx, s.Store.R, qid)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load question", err)
	}
	if len(q.Problem.Curated) == 0 {
		return nil, httpapi.ErrNotFound
	}
	cq, err := s.curatedOf(&q.Problem)
	if err != nil {
		return nil, s.plInternalErr(ctx, "decode question", err)
	}
	h := cq.Hint.Get(lang)
	if h == "" {
		return nil, httpapi.ErrNotFound
	}
	return api.RequestHint200JSONResponse{Hint: h}, nil
}
