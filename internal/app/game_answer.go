package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
	"github.com/Perrin-Nj/numerix-api/internal/visual"
)

// storedAnswer is the JSON kept in answers.json. It is enough to replay the same response to a retry
// (same Idempotency-Key) without grading twice.
type storedAnswer struct {
	Given      engine.Value `json:"given"`
	Outcome    string       `json:"outcome"`
	Code       string       `json:"code_erreur,omitempty"`
	ResponseMs int          `json:"response_ms,omitempty"`
	Skill      string       `json:"skill_id"`
	Mastered   bool         `json:"mastered,omitempty"` // this answer made the skill cross the mastery threshold
	Rewards    []api.Reward `json:"rewards"`
	// GivenRaw is the answer exactly as sent, kept for curated questions (their answers can be lists of ids).
	GivenRaw json.RawMessage `json:"given_raw,omitempty"`
	// RetryOpen is true when this wrong first answer was answered with attempt 1: the reveal was withheld and a second
	// try is open until answer_retries has a row for the beat.
	RetryOpen bool `json:"retry_open,omitempty"`
	// PendingFinish is true when this wrong answer was the last of the chapter: the chapter stays active (no card, no
	// completion) until the second try is answered or the child moves on.
	PendingFinish bool `json:"pending_finish,omitempty"`
}

// AnswerBeat answers POST /sessions/{sessionId}/beats/{beatId}/answer: grade, update mastery, close the chapter
// after the last beat, and return the verdict with the how-to picture. All of it in one write transaction.
func (s *Server) AnswerBeat(ctx context.Context, req api.AnswerBeatRequestObject) (api.AnswerBeatResponseObject, error) {
	r, c, err := s.loadSession(ctx, plUID(req.SessionId))
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpapi.Validation(plField("answer", "required"))
	}
	given, err := engineValue(req.Body.Answer)
	if err != nil {
		return nil, httpapi.Validation(plField("answer", "unreadable"))
	}
	g := s.game()
	lang := gameLang(req.Params.AcceptLanguage, c.Locale)
	beatID, idem := plUID(req.BeatId), plUID(req.Params.IdempotencyKey)
	ms := 0
	if req.Body.ResponseTimeMs != nil {
		ms = max(0, *req.Body.ResponseTimeMs)
	}
	attempt := 0 // 0 = legacy client: full reveal at once, no second try
	if a := req.Body.Attempt; a != nil {
		if !a.Valid() {
			return nil, httpapi.Validation(plField("attempt", "1 or 2"))
		}
		attempt = int(*a)
	}
	retryOn := s.plConfig(ctx).RetryEnabled
	if attempt == 2 {
		return s.answerRetry(ctx, r, c, req, lang, retryOn)
	}

	// B2: the client's recent scene ids, validated before any write; nil means "leave the stored list alone"
	var variants []string
	if req.Body.VariantHistory != nil {
		v, msg := cleanVariants(*req.Body.VariantHistory)
		if msg != "" {
			return nil, httpapi.Validation(plField("variant_history", msg))
		}
		variants = append([]string{}, v...) // non-nil even when empty, so an empty list clears the history
	}

	var stored storedAnswer
	var problem engine.Problem
	var cur *content.Question // the curated question, nil for a generated one
	var curRes content.Result
	rawGiven, _ := req.Body.Answer.MarshalJSON() // the answer as JSON; an empty union gives null and is graded as a random error
	var finished, replay bool
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		// 1. the beat must belong to this session
		var qid string
		if e := tx.QueryRowContext(ctx, `SELECT question_id FROM beats WHERE id = ? AND session_id = ?`, beatID, r.ID).Scan(&qid); errors.Is(e, sql.ErrNoRows) {
			return httpapi.ErrNotFound
		} else if e != nil {
			return e
		}
		q, e := loadQuestion(ctx, tx, qid)
		if e != nil {
			return e
		}
		problem = q.Problem
		if len(problem.Curated) > 0 {
			if cur, e = s.curatedOf(&problem); e != nil {
				return e
			}
		}
		// 2. already answered? same key replays the first result, another key is a conflict
		var prevKey sql.NullString
		var raw string
		switch e := tx.QueryRowContext(ctx, `SELECT idempotency_key, json FROM answers WHERE beat_id = ? LIMIT 1`, beatID).Scan(&prevKey, &raw); {
		case e == nil:
			if prevKey.String != idem {
				return plConflict("beat_answered", "This question was already answered.")
			}
			replay = true
			if e := json.Unmarshal([]byte(raw), &stored); e != nil {
				return e
			}
			if cur != nil {
				curRes = cur.Check(stored.GivenRaw) // deterministic, so a replay shows the same explanation
			}
			return nil
		case !errors.Is(e, sql.ErrNoRows):
			return e
		}
		// 3. the chapter must be playable
		var status string
		if e := tx.QueryRowContext(ctx, `SELECT status FROM sessions WHERE id = ?`, r.ID).Scan(&status); e != nil {
			return e
		}
		switch status {
		case "paused":
			return plConflict("session_paused", "This chapter is paused.")
		case "completed", "abandoned":
			return plConflict("session_completed", "This chapter is already complete.")
		}
		// 4. grade (deterministic, no LLM) and update mastery
		now := g.now()
		var res engine.Result
		if cur != nil {
			curRes = cur.Check(rawGiven)
			res = engine.Result{Type: engine.Outcome(curRes.Outcome), ReponseDonnee: given, CompetenceID: cur.Skill, CodeErreur: curRes.Misconception}
			if curRes.Misconception != "" {
				res.DescriptionErreur = s.misconceptionDesc(ctx, tx, curRes.Misconception)
			}
		} else {
			res = problem.Evaluate(given)
		}
		prof, e := s.profileFrom(ctx, tx, r.ChildID)
		if e != nil {
			return e
		}
		skill := problem.CompetenceID
		before := prof.Mastery(skill, now)
		target := 0.0
		if t, ok := problem.Metadonnees["temps_cible_ms"].(float64); ok {
			target = t
		} else if t, ok := problem.Metadonnees["temps_cible_ms"].(int); ok {
			target = float64(t)
		}
		after := prof.Observe(skill, res, float64(ms), target, now) // only the first attempt ever reaches mastery
		qType := problem.Shape
		if cur != nil {
			qType = cur.Type
		}
		stored = storedAnswer{RetryOpen: attempt == 1 && retryOn && res.Type != engine.Correct && qType != "truefalse", Given: given, Outcome: string(res.Type), Code: res.CodeErreur, ResponseMs: ms, Skill: skill,
			Mastered: before < engine.MasteryThreshold && after >= engine.MasteryThreshold, Rewards: []api.Reward{}}
		if cur != nil {
			stored.GivenRaw = rawGiven
		}
		// 5. last beat? close the chapter and decide the rewards
		var answered int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM answers WHERE session_id = ?`, r.ID).Scan(&answered); e != nil {
			return e
		}
		answered++ // this answer
		if answered >= r.Data.Length {
			if stored.RetryOpen { // a wrong last answer with a second try open: the chapter waits for the window to close
				stored.PendingFinish = true
			} else {
				finished = true
				good := 0
				if res.Type == engine.Correct {
					good = 1
				}
				rw, e := s.finishChapter(ctx, tx, r, c, now, skill, 1, good)
				if e != nil {
					return e
				}
				stored.Rewards = append(stored.Rewards, rw...)
			}
		}
		if stored.Mastered {
			sk := skill
			stored.Rewards = append(stored.Rewards, api.Reward{Type: api.SkillMastered, SkillId: &sk})
		}
		// 6. persist the answer and the profile
		if cur != nil { // per question statistics for the content team (Mongo, k-anonymous)
			if e := s.Sync.Enqueue(ctx, tx, mongosync.KindQuestionStats, cur.ID, ""); e != nil {
				return e
			}
		}
		if variants != nil { // json_set rewrites one key in place, so a concurrent session update is not lost
			vj, _ := json.Marshal(variants) // a string slice always marshals
			if _, e := tx.ExecContext(ctx, `UPDATE sessions SET json = json_set(json, '$.variants', json(?)) WHERE id = ?`, string(vj), r.ID); e != nil {
				return e
			}
		}
		aj, e := json.Marshal(stored)
		if e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO answers (id, beat_id, session_id, idempotency_key, outcome, json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			newID(), beatID, r.ID, idem, stored.Outcome, string(aj), plTS(now)); e != nil {
			return e
		}
		return saveProfile(ctx, tx, prof, now)
	})
	var ae *httpapi.APIError
	if errors.As(err, &ae) {
		return nil, err
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "answer beat", err)
	}
	var retryUsed bool // the second try was already taken: the first answer replays as a full reveal
	if attempt == 1 && stored.RetryOpen {
		if e := s.Store.R.QueryRowContext(ctx, `SELECT COUNT(*) > 0 FROM answer_retries WHERE beat_id = ?`, beatID).Scan(&retryUsed); e != nil {
			return nil, s.plInternalErr(ctx, "check retry", e)
		}
	}
	withheld := attempt == 1 && stored.RetryOpen && !retryUsed
	if finished && !replay { // a finished chapter may complete accepted challenges (feed module)
		if extra, e := s.settleChallenges(ctx, c); e != nil {
			s.Log.Warn("settle challenges failed", "err", e)
		} else if len(extra) > 0 {
			stored.Rewards = append(stored.Rewards, extra...)
			if withheld { // the reveal comes with the second try: keep the extra rewards for it
				if aj, e := json.Marshal(stored); e == nil {
					_, _ = s.Store.W.ExecContext(ctx, `UPDATE answers SET json = ? WHERE beat_id = ?`, string(aj), beatID)
				}
			}
		}
	}
	sess, err := s.sessionByID(ctx, r.ID, lang, false, true)
	if err != nil {
		return nil, err
	}
	if withheld { // first wrong answer with a second try open: the verdict only, nothing that gives the answer away
		one, open := api.AnswerResultAttemptN1, true
		return api.AnswerBeat200JSONResponse(api.AnswerResult{
			Outcome: api.Outcome(stored.Outcome), Attempt: &one, RetryAvailable: &open,
			Denouement: api.Narration{Status: api.Pending}, Session: sess, Rewards: &[]api.Reward{},
		}), nil
	}
	out := s.revealResult(ctx, sess, stored, problem, cur, curRes, lang)
	if attempt == 1 {
		one := api.AnswerResultAttemptN1
		out.Attempt = &one
	}
	return api.AnswerBeat200JSONResponse(out), nil
}

// revealResult builds the full verdict: the answer, the explanation, the feedback and the how-to picture.
func (s *Server) revealResult(ctx context.Context, sess api.Session, stored storedAnswer, problem engine.Problem, cur *content.Question, curRes content.Result, lang string) api.AnswerResult {
	ca := answerValue(problem.Reponse)
	out := api.AnswerResult{
		Outcome:       api.Outcome(stored.Outcome),
		CorrectAnswer: &ca,
		Denouement:    api.Narration{Status: api.Pending},
		Session:       sess,
		Rewards:       &stored.Rewards,
		SceneKey:      sceneKeyPtr(sceneKeyFor(stored.Skill, stored.Outcome, stored.Code)),
	}
	if cur != nil {
		ca = answerFromJSON(cur.CorrectAnswer(lang))
		if ex := cur.Explain.Get(lang); ex != "" {
			out.Explain = &ex
		}
		if curRes.Misconception != "" {
			code := curRes.Misconception
			out.Misconception = &code
		}
		if fb := curRes.Feedback.Get(lang); fb != "" {
			out.Feedback = &fb
		}
	}
	if mv := s.methodVisual(ctx, problem.Visual, lang); mv != nil {
		out.MethodVisual = mv
	}
	if cur != nil { // only a released reveal reaches here: the withheld first result of a retry window returns earlier
		if _, rv := s.sceneParts(ctx, cur.ID); rv != nil {
			out.SceneReveal = &rv
		}
	}
	return out
}

// methodVisual builds the how-to picture for a question (nil when none exists) and caches it by cache_key,
// so the next child with the same numbers reuses the stored JSON.
func (s *Server) methodVisual(ctx context.Context, hint *engine.VisualHint, lang string) *api.MethodVisual {
	if hint == nil {
		return nil
	}
	built, ok := visual.Build(hint.Kind, hint.Params, lang)
	if !ok {
		return nil
	}
	var cached string
	if err := s.Store.R.QueryRowContext(ctx, `SELECT json FROM question_visuals WHERE cache_key = ?`, built.CacheKey).Scan(&cached); err == nil {
		var mv api.MethodVisual
		if json.Unmarshal([]byte(cached), &mv) == nil {
			return &mv
		}
	}
	raw, _ := json.Marshal(built) // same JSON shape as the contract's MethodVisual
	var mv api.MethodVisual
	if err := json.Unmarshal(raw, &mv); err != nil {
		return nil
	}
	_ = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error { // best effort: a failed cache write must not fail the answer
		_, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO question_visuals (cache_key, json) VALUES (?, ?)`, built.CacheKey, string(raw))
		return e
	})
	return &mv
}
