package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/narrator"
)

// rowQuerier is satisfied by *sql.DB and *sql.Tx, so helpers work inside and outside a transaction.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// loadProfile reads the child's BKT profile (JSON in child_profiles); a missing row is an empty profile.
func (s *Server) loadProfile(ctx context.Context, childID string) (*engine.Profile, error) {
	return s.profileFrom(ctx, s.Store.R, childID)
}

// profileFrom is loadProfile on any querier (the answer path reads it inside its write transaction).
func (s *Server) profileFrom(ctx context.Context, q rowQuerier, childID string) (*engine.Profile, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT bkt FROM child_profiles WHERE child_id = ?`, childID).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, s.plInternalErr(ctx, "load profile", err)
	}
	prof, perr := engine.ParseProfile(childID, raw, s.game().graph)
	if perr != nil {
		return nil, s.plInternalErr(ctx, "decode profile", perr)
	}
	return prof, nil
}

// saveProfile upserts the profile inside a write transaction.
func saveProfile(ctx context.Context, tx *sql.Tx, prof *engine.Profile, now time.Time) error {
	raw, err := prof.JSON()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO child_profiles (child_id, bkt, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(child_id) DO UPDATE SET bkt = excluded.bkt, updated_at = excluded.updated_at`, prof.ChildID, raw, plTS(now))
	return err
}

// StartSession answers POST /children/{childId}/sessions: pick the skill (given, or the planner's choice) and open a chapter.
func (s *Server) StartSession(ctx context.Context, req api.StartSessionRequestObject) (api.StartSessionResponseObject, error) {
	c, _, err := s.gameChild(ctx, plUID(req.ChildId), true)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	g := s.game()
	world := string(req.Body.World)
	if ok, werr := s.worldExists(ctx, world); werr != nil {
		return nil, werr
	} else if !ok {
		return nil, httpapi.Validation(plField("world", "unknown world"))
	}
	if !slices.Contains(s.enabledWorlds(ctx, c.ID), world) { // a parent turned this world off
		return nil, httpapi.Validation(plField("world", "not enabled for this child"))
	}
	s.syncBank(ctx) // fresh view of which skills have bank questions
	lang := gameLang(req.Params.AcceptLanguage, c.Locale)
	idem := plUID(req.Params.IdempotencyKey)
	// a retry with the same Idempotency-Key returns the session created the first time
	var prior string
	if e := s.Store.R.QueryRowContext(ctx, `SELECT id FROM sessions WHERE child_id = ? AND json_extract(json,'$.idem') = ?`, c.ID, idem).Scan(&prior); e == nil {
		view, verr := s.sessionByID(ctx, prior, lang, true, false)
		return api.StartSession201JSONResponse(view), verr
	}
	now := g.now()
	prof, err := s.loadProfile(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	skill, reason := "", "choisie"
	if req.Body.SkillId != nil {
		sk, gerr := g.graph.Get(*req.Body.SkillId)
		if gerr != nil || !g.graph.PlayableFor(world, sk.ID, gradeOf(c)) { // also refuses a chapter this grade has too few questions for
			return nil, httpapi.Validation(plField("skill_id", "unknown or not available yet"))
		}
		// Same lock rule as the journey map (QA-BE-004): a chapter whose prerequisites are not mastered is refused with 422.
		// skillState is shared with the journey so the two can never disagree (Spring: one @Service method, two controllers).
		mastered := prof.Mastered(now)
		due := map[string]bool{}
		for _, d := range prof.ToReview(now) {
			due[d.SkillID] = true
		}
		if g.skillState(prof, sk.ID, mastered, due) == api.NodeStateLocked && !g.graph.IsBankOnly(world) {
			return nil, httpapi.Validation(plField("skill_id", "locked: "+string(api.Prerequisites))) // locked_reason = prerequisites
		}
		skill = sk.ID
	} else {
		sel, cerr := engine.Choose(g.graph, prof, world, gradeOf(c), engine.NewRNG(g.seed()), now)
		if cerr != nil {
			return nil, httpapi.Validation(plField("world", "no chapter available for this world and grade"))
		}
		skill, reason = sel.SkillID, sel.Reason
	}
	seed := uint32(g.seed())
	data := sessData{Kind: string(req.Body.Kind), Skill: skill, Seed: seed, Length: chapterLength, TitleIdx: narrator.TitleIndex(seed), Reason: reason, Lang: lang, Idem: idem}
	raw, _ := json.Marshal(data)
	id := newID()
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		// one open chapter per child: older open ones are closed as abandoned
		if _, e := tx.ExecContext(ctx, `UPDATE sessions SET status = 'abandoned', finished_at = ? WHERE child_id = ? AND status IN ('active','paused')`, plTS(now), c.ID); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO sessions (id, child_id, status, world_id, json, started_at) VALUES (?, ?, 'active', ?, ?, ?)`,
			id, c.ID, world, string(raw), plTS(now))
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "create session", err)
	}
	view, err := s.sessionByID(ctx, id, lang, false, false)
	if err != nil {
		return nil, err
	}
	return api.StartSession201JSONResponse(view), nil
}

// sessionByID loads a session row (no ownership check; callers already did it) and renders it.
func (s *Server) sessionByID(ctx context.Context, id, lang string, withBeat, withScore bool) (api.Session, error) {
	var r sessRow
	var raw string
	err := s.Store.R.QueryRowContext(ctx, `SELECT id, child_id, status, COALESCE(world_id,''), json, started_at, paused_at, finished_at
		FROM sessions WHERE id = ?`, id).Scan(&r.ID, &r.ChildID, &r.Status, &r.World, &raw, &r.Started, &r.Paused, &r.Finished)
	if err != nil {
		return api.Session{}, s.plInternalErr(ctx, "reload session", err)
	}
	if err := json.Unmarshal([]byte(raw), &r.Data); err != nil {
		return api.Session{}, s.plInternalErr(ctx, "decode session", err)
	}
	return s.sessionView(ctx, r, lang, withBeat, withScore)
}

// outcomesOf lists the outcomes of a session's answers in order.
func (s *Server) outcomesOf(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := s.Store.R.QueryContext(ctx, `SELECT COALESCE(outcome,'') FROM answers WHERE session_id = ? ORDER BY created_at, rowid`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// scoreOf computes the first-try score (contract: pct = round(1000*correct/total)/10).
func (s *Server) scoreOf(ctx context.Context, status string, outcomes []string) *api.Score {
	correct := 0
	for _, o := range outcomes {
		if o == string(engine.Correct) {
			correct++
		}
	}
	total := len(outcomes)
	pct := 0.0
	if total > 0 {
		pct = math.Round(1000*float64(correct)/float64(total)) / 10
	}
	thr := s.plConfig(ctx).CardThresholdPct
	return &api.Score{Correct: correct, Total: total, Pct: float32(pct), ThresholdPct: thr, CardAwarded: status == "completed" && total > 0 && pct >= float64(thr)}
}

// sessionView renders the contract's Session. withBeat adds the open beat, withScore adds the score.
func (s *Server) sessionView(ctx context.Context, r sessRow, lang string, withBeat, withScore bool) (api.Session, error) {
	outcomes, err := s.outcomesOf(ctx, r.ID)
	if err != nil {
		return api.Session{}, s.plInternalErr(ctx, "load answers", err)
	}
	title := narrator.TitleOf(r.World, r.Data.TitleIdx, lang)
	out := api.Session{
		Id: plUUID(r.ID), ChildId: plUUID(r.ChildID), World: api.WorldId(r.World), Kind: api.SessionKind(r.Data.Kind),
		Status: api.SessionStatus(r.Status), ChapterLength: r.Data.Length, BeatsAnswered: len(outcomes),
		StoryTitle: &title, StartedAt: plParseTS(r.Started),
		VariantHistory: plPtr(append([]string{}, r.Data.Variants...)), // always an array, never null
	}
	if r.Status == "completed" {
		out.EndedAt = plNullTime(r.Finished)
	}
	if withScore {
		out.Score = s.scoreOf(ctx, r.Status, outcomes)
	}
	if withBeat {
		b, found, err := s.openBeat(ctx, r.ID)
		if err != nil {
			return api.Session{}, err
		}
		if found {
			view, err := s.beatView(ctx, b, lang)
			if err != nil {
				return api.Session{}, err
			}
			out.CurrentBeat = &view
		}
	}
	return out, nil
}

// openBeat returns the unanswered beat of a session, if any.
func (s *Server) openBeat(ctx context.Context, sessionID string) (beatRow, bool, error) {
	return openBeatFrom(ctx, s.Store.R, sessionID)
}

// openBeatFrom is openBeat on any querier.
func openBeatFrom(ctx context.Context, q rowQuerier, sessionID string) (beatRow, bool, error) {
	var b beatRow
	var qid sql.NullString
	err := q.QueryRowContext(ctx, `SELECT b.id, b.session_id, b.idx, b.question_id, COALESCE(json_extract(b.json,'$.shape'),''), b.created_at
		FROM beats b WHERE b.session_id = ? AND NOT EXISTS (SELECT 1 FROM answers a WHERE a.beat_id = b.id)
		ORDER BY b.idx DESC LIMIT 1`, sessionID).Scan(&b.ID, &b.SessionID, &b.Index, &qid, &b.Shape, &b.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return beatRow{}, false, nil
	}
	b.QuestionID = qid.String
	return b, err == nil, err
}

// beatView renders a Beat for the child: the statement and the choices, never the answer.
func (s *Server) beatView(ctx context.Context, b beatRow, lang string) (api.Beat, error) {
	q, err := loadQuestion(ctx, s.Store.R, b.QuestionID)
	if err != nil {
		return api.Beat{}, s.plInternalErr(ctx, "load question", err)
	}
	if len(q.Problem.Curated) > 0 { // a question from the bank: its own story, options and pictures
		cq, cerr := s.curatedOf(&q.Problem)
		if cerr != nil {
			return api.Beat{}, s.plInternalErr(ctx, "decode curated question", cerr)
		}
		return s.curatedBeatView(ctx, b, cq, lang)
	}
	choices := make([]api.AnswerValue, len(q.Problem.Choix))
	for i, c := range q.Problem.Choix {
		choices[i] = answerValue(c)
	}
	created := plParseTS(b.Created)
	return api.Beat{
		Id: plUUID(b.ID), SessionId: plUUID(b.SessionID), Index: b.Index, SkillId: q.Problem.CompetenceID,
		Question:  api.Question{MathText: localMath(&q.Problem, lang), Choices: choices},
		Scene:     api.Narration{Status: api.Pending},
		SceneKey:  sceneKeyPtr(sceneFamily(q.Problem.CompetenceID)), // the wrong-answer part is known only after the answer
		CreatedAt: &created,
	}, nil
}

// ListSessions answers GET /children/{childId}/sessions (newest first, simple cursor on started_at).
func (s *Server) ListSessions(ctx context.Context, req api.ListSessionsRequestObject) (api.ListSessionsResponseObject, error) {
	c, _, err := s.gameChild(ctx, plUID(req.ChildId), false)
	if err != nil {
		return nil, err
	}
	lang := gameLang(req.Params.AcceptLanguage, c.Locale)
	limit := 20
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		limit = min(*req.Params.Limit, 100)
	}
	cursor := ""
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	rows, err := s.Store.R.QueryContext(ctx, `SELECT id FROM sessions WHERE child_id = ? AND (? = '' OR started_at < ?)
		ORDER BY started_at DESC, id LIMIT ?`, c.ID, cursor, cursor, limit+1)
	if err != nil {
		return nil, s.plInternalErr(ctx, "list sessions", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, s.plInternalErr(ctx, "scan session", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	var next *string
	if len(ids) > limit { // one extra row means another page exists
		ids = ids[:limit]
		var started string
		if err := s.Store.R.QueryRowContext(ctx, `SELECT started_at FROM sessions WHERE id = ?`, ids[len(ids)-1]).Scan(&started); err == nil {
			next = &started
		}
	}
	items := make([]api.Session, 0, len(ids))
	for _, id := range ids {
		v, err := s.sessionByID(ctx, id, lang, true, false)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return api.ListSessions200JSONResponse{Items: items, NextCursor: next}, nil
}

// GetSession answers GET /sessions/{sessionId}.
func (s *Server) GetSession(ctx context.Context, req api.GetSessionRequestObject) (api.GetSessionResponseObject, error) {
	r, c, err := s.loadSession(ctx, plUID(req.SessionId))
	if err != nil {
		return nil, err
	}
	v, err := s.sessionView(ctx, r, gameLang(req.Params.AcceptLanguage, c.Locale), true, r.Status == "completed")
	if err != nil {
		return nil, err
	}
	return api.GetSession200JSONResponse(v), nil
}

// setStatus moves a session between states. allowed lists the states it may leave.
func (s *Server) setStatus(ctx context.Context, sessionID, to string, allowed ...string) (sessRow, plChild, error) {
	r, c, err := s.loadSession(ctx, sessionID)
	if err != nil {
		return r, c, err
	}
	if r.Status == to {
		return r, c, nil // already there: the call is idempotent
	}
	if !slices.Contains(allowed, r.Status) {
		return r, c, plConflict("session_"+r.Status, "This chapter is "+r.Status+".")
	}
	now := plTS(s.game().now())
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		switch to {
		case "paused":
			_, e := tx.ExecContext(ctx, `UPDATE sessions SET status = 'paused', paused_at = ? WHERE id = ?`, now, r.ID)
			return e
		case "active":
			_, e := tx.ExecContext(ctx, `UPDATE sessions SET status = 'active', paused_at = NULL WHERE id = ?`, r.ID)
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE sessions SET status = 'completed', finished_at = ? WHERE id = ?`, now, r.ID); e != nil {
			return e
		}
		return s.syncChapterDone(ctx, tx, r.ChildID, c.ParentID, r.World, r.Data.Skill) // Mongo copy, same transaction
	})
	if err != nil {
		return r, c, s.plInternalErr(ctx, "update session", err)
	}
	r.Status = to
	return r, c, nil
}

// PauseSession answers POST /sessions/{sessionId}/pause.
func (s *Server) PauseSession(ctx context.Context, req api.PauseSessionRequestObject) (api.PauseSessionResponseObject, error) {
	r, c, err := s.setStatus(ctx, plUID(req.SessionId), "paused", "active")
	if err != nil {
		return nil, err
	}
	v, err := s.sessionByID(ctx, r.ID, gameLang(req.Params.AcceptLanguage, c.Locale), true, false)
	return api.PauseSession200JSONResponse(v), err
}

// ResumeSession answers POST /sessions/{sessionId}/resume.
func (s *Server) ResumeSession(ctx context.Context, req api.ResumeSessionRequestObject) (api.ResumeSessionResponseObject, error) {
	r, c, err := s.setStatus(ctx, plUID(req.SessionId), "active", "paused")
	if err != nil {
		return nil, err
	}
	v, err := s.sessionByID(ctx, r.ID, gameLang(req.Params.AcceptLanguage, c.Locale), true, false)
	return api.ResumeSession200JSONResponse(v), err
}

// FinishSession answers POST /sessions/{sessionId}/finish: ends the chapter early.
func (s *Server) FinishSession(ctx context.Context, req api.FinishSessionRequestObject) (api.FinishSessionResponseObject, error) {
	r, c, err := s.setStatus(ctx, plUID(req.SessionId), "completed", "active", "paused")
	if err != nil {
		return nil, err
	}
	v, err := s.sessionByID(ctx, r.ID, gameLang(req.Params.AcceptLanguage, c.Locale), false, true)
	return api.FinishSession200JSONResponse(v), err
}

// NextBeat answers POST /sessions/{sessionId}/beats: the open beat if there is one (200), else a new one (201).
func (s *Server) NextBeat(ctx context.Context, req api.NextBeatRequestObject) (api.NextBeatResponseObject, error) {
	r, c, err := s.loadSession(ctx, plUID(req.SessionId))
	if err != nil {
		return nil, err
	}
	if _, _, err := s.gameChild(ctx, r.ChildID, true); err != nil { // consent and subscription still hold
		return nil, err
	}
	lang := gameLang(req.Params.AcceptLanguage, c.Locale)
	switch r.Status {
	case "completed", "abandoned":
		return nil, plConflict("session_completed", "This chapter is already complete.")
	case "paused":
		return nil, plConflict("session_paused", "This chapter is paused.")
	}
	var created bool
	var beat beatRow
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		if b, found, e := openBeatFrom(ctx, tx, r.ID); e != nil || found { // a retry gets the same beat
			beat = b
			return e
		}
		var n int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM beats WHERE session_id = ?`, r.ID).Scan(&n); e != nil {
			return e
		}
		if n >= r.Data.Length {
			return plConflict("session_completed", "This chapter is already complete.")
		}
		var last string
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(json_extract(json,'$.shape'),'') FROM beats WHERE session_id = ? ORDER BY idx DESC LIMIT 1`, r.ID).Scan(&last)
		qid, shape, e := s.pickQuestion(ctx, tx, r.ChildID, r.World, r.Data.Skill, gradeOf(c), last)
		if e != nil {
			return e
		}
		bj, _ := json.Marshal(map[string]string{"shape": shape})
		beat = beatRow{ID: newID(), SessionID: r.ID, Index: n, QuestionID: qid, Shape: shape, Created: plTS(s.game().now())}
		_, e = tx.ExecContext(ctx, `INSERT INTO beats (id, session_id, idx, question_id, json, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			beat.ID, r.ID, n, qid, string(bj), beat.Created)
		created = e == nil
		return e
	})
	var ae *httpapi.APIError
	if errors.As(err, &ae) {
		return nil, err
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "next beat", err)
	}
	view, err := s.beatView(ctx, beat, lang)
	if err != nil {
		return nil, err
	}
	if created {
		return api.NextBeat201JSONResponse(view), nil
	}
	return api.NextBeat200JSONResponse(view), nil
}

// paramsHash identifies a question in the bank: same skill and same statement means the same question.
func paramsHash(skill, statement string) string {
	sum := sha256.Sum256([]byte(skill + "|" + statement))
	return hex.EncodeToString(sum[:])
}

// pickQuestion is the question selection, run inside ONE write transaction:
//  0. the curated bank is primary: the best published question of this quest (world + skill) the child has not been
//     served, by child grade, target difficulty and a different type than the previous beat (pickCurated);
//  1. only when the child exhausted the bank (or the skill has none): the generator, via a cache of generated
//     questions, a fresh draw, or as the last resort the oldest question re-served (pickGenerated);
//
// then record "served" in child_questions. SQLite has a single writer, so no SKIP LOCKED is needed
// (Postgres would use FOR UPDATE SKIP LOCKED here).
func (s *Server) pickQuestion(ctx context.Context, tx *sql.Tx, childID, world, skill string, grade int, avoidShape string) (id, shape string, err error) {
	now := plTS(s.game().now())
	id, shape, curated, err := s.pickCurated(ctx, tx, childID, world, skill, grade, avoidShape)
	if err != nil {
		return "", "", err
	}
	if !curated {
		var reserved bool
		if id, shape, reserved, err = s.pickGenerated(ctx, tx, childID, world, skill, avoidShape, now); err != nil {
			return "", "", err
		}
		if reserved { // a question the child already saw was served again; its record is updated
			return id, shape, nil
		}
	}
	// record "served": the primary key (child_id, question_id) is the safety net against double serving
	if _, err = tx.ExecContext(ctx, `INSERT INTO child_questions (child_id, question_id, served_at) VALUES (?, ?, ?)`, childID, id, now); err != nil {
		return "", "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE questions SET served_count = served_count + 1 WHERE id = ?`, id); err != nil {
		return "", "", err
	}
	return id, shape, nil
}

// bankOnlyWorlds are the worlds whose questions all come from the written bank (each has its own diagram and reaction).
// When a child has seen every bank question of a skill there, an old one is served again; a generated question,
// which has no matching diagram or reaction, is only the last resort.
var bankOnlyWorlds = []string{"echecs", "medecine", "espace"}

// reserveBank serves again the least recently seen bank question of this skill in this world (ok false when the child has seen none).
func (s *Server) reserveBank(ctx context.Context, tx *sql.Tx, childID, world, skill, avoidShape, now string) (id, shape string, ok bool, err error) {
	err = tx.QueryRowContext(ctx, `SELECT q.id, q.shape FROM questions q JOIN child_questions cq ON cq.question_id = q.id
		WHERE q.skill_id = ? AND cq.child_id = ? AND q.bank_id IN (SELECT id FROM bank_questions WHERE world_id = ? AND status = 'published')
		ORDER BY (q.shape = ?), cq.served_at ASC LIMIT 1`, skill, childID, world, avoidShape).Scan(&id, &shape)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil // nothing seen yet: the caller may still generate
	}
	if err != nil {
		return "", "", false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE child_questions SET served_at = ? WHERE child_id = ? AND question_id = ?`, now, childID, id); err != nil {
		return "", "", false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE questions SET served_count = served_count + 1 WHERE id = ?`, id)
	return id, shape, true, err
}

// pickGenerated is the generator side of pickQuestion. reserved is true when it re-served an old question.
func (s *Server) pickGenerated(ctx context.Context, tx *sql.Tx, childID, world, skill, avoidShape, now string) (id, shape string, reserved bool, err error) {
	if slices.Contains(bankOnlyWorlds, world) { // the written bank first, even when its questions repeat
		if rid, rshape, ok, rerr := s.reserveBank(ctx, tx, childID, world, skill, avoidShape, now); rerr != nil || ok {
			return rid, rshape, ok, rerr
		}
	}
	g := s.game()
	// 1. a generated question stored earlier that this child has not seen, of another shape than the last beat
	err = tx.QueryRowContext(ctx, `SELECT q.id, q.shape FROM questions q
		WHERE q.skill_id = ? AND q.origin = 'generated' AND (? = '' OR q.shape <> ?)
		  AND NOT EXISTS (SELECT 1 FROM child_questions cq WHERE cq.child_id = ? AND cq.question_id = q.id)
		ORDER BY q.served_count ASC, RANDOM() LIMIT 1`, skill, avoidShape, avoidShape, childID).Scan(&id, &shape)
	if err == nil {
		return id, shape, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", false, err
	}
	// 2. generate a fresh question (also unseen and of another shape) and store it
	unseen := func(p *engine.Problem) bool { // accept only statements this child has not seen
		var one int
		e := tx.QueryRowContext(ctx, `SELECT 1 FROM questions q JOIN child_questions cq ON cq.question_id = q.id
			WHERE q.params_hash = ? AND cq.child_id = ?`, paramsHash(skill, p.EnonceMath), childID).Scan(&one)
		return errors.Is(e, sql.ErrNoRows)
	}
	p, gerr := g.graph.GenerateFresh(skill, engine.NewRNG(g.seed()), avoidShape, unseen)
	switch {
	case gerr == nil:
		raw, merr := json.Marshal(p)
		if merr != nil {
			return "", "", false, merr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO questions (id, params_hash, skill_id, json, shape) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(params_hash) DO NOTHING`, newID(), paramsHash(skill, p.EnonceMath), skill, string(raw), p.Shape); err != nil {
			return "", "", false, err
		}
		err = tx.QueryRowContext(ctx, `SELECT id, shape FROM questions WHERE params_hash = ?`, paramsHash(skill, p.EnonceMath)).Scan(&id, &shape)
		return id, shape, false, err
	case errors.Is(gerr, engine.ErrNoFreshProblem), errors.Is(gerr, engine.ErrNotImplemented):
		// 3. everything seen, or no generator for this skill: re-serve the oldest question of this world, preferring another shape
		err = tx.QueryRowContext(ctx, `SELECT q.id, q.shape FROM questions q JOIN child_questions cq ON cq.question_id = q.id
			WHERE q.skill_id = ? AND cq.child_id = ? AND (q.origin = 'generated' OR q.bank_id IN (SELECT id FROM bank_questions WHERE world_id = ?))
			ORDER BY (q.shape = ?), cq.served_at ASC LIMIT 1`, skill, childID, world, avoidShape).Scan(&id, &shape)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", false, plConflict("no_question_available", "No question is available for this chapter yet.")
		}
		if err != nil {
			return "", "", false, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE child_questions SET served_at = ? WHERE child_id = ? AND question_id = ?`, now, childID, id); err != nil {
			return "", "", false, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE questions SET served_count = served_count + 1 WHERE id = ?`, id)
		return id, shape, true, err
	}
	return "", "", false, gerr
}
