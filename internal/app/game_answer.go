package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/narrator"
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

	var stored storedAnswer
	var problem engine.Problem
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
		// 2. already answered? same key replays the first result, another key is a conflict
		var prevKey sql.NullString
		var raw string
		switch e := tx.QueryRowContext(ctx, `SELECT idempotency_key, json FROM answers WHERE beat_id = ? LIMIT 1`, beatID).Scan(&prevKey, &raw); {
		case e == nil:
			if prevKey.String != idem {
				return plConflict("beat_answered", "This question was already answered.")
			}
			replay = true
			return json.Unmarshal([]byte(raw), &stored)
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
		res := problem.Evaluate(given)
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
		after := prof.Observe(skill, res, float64(ms), target, now)
		stored = storedAnswer{Given: given, Outcome: string(res.Type), Code: res.CodeErreur, ResponseMs: ms, Skill: skill,
			Mastered: before < engine.MasteryThreshold && after >= engine.MasteryThreshold, Rewards: []api.Reward{}}
		// 5. last beat? close the chapter and decide the rewards
		var answered int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM answers WHERE session_id = ?`, r.ID).Scan(&answered); e != nil {
			return e
		}
		answered++ // this answer
		if finished = answered >= r.Data.Length; finished {
			var correct int
			if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM answers WHERE session_id = ? AND outcome = 'correct'`, r.ID).Scan(&correct); e != nil {
				return e
			}
			if res.Type == engine.Correct {
				correct++
			}
			stored.Rewards = append(stored.Rewards, api.Reward{Type: api.ChapterComplete})
			pct := float64(correct) / float64(answered) * 100
			if pct >= float64(s.plConfig(ctx).CardThresholdPct) {
				card := api.StoryCard{Id: plUUID(newID()), World: api.WorldId(r.World), Title: narrator.TitleOf(r.World, r.Data.TitleIdx, r.Data.Lang),
					ImageUrl: "", SessionId: &req.SessionId, EarnedAt: now}
				cj, _ := json.Marshal(map[string]string{"world": r.World, "title": card.Title, "session_id": r.ID, "image_url": ""})
				if _, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO collection (child_id, card_id, json, acquired_at) VALUES (?, ?, ?, ?)`,
					r.ChildID, plUID(card.Id), string(cj), plTS(now)); e != nil {
					return e
				}
				stored.Rewards = append(stored.Rewards, api.Reward{Type: api.CardEarned, Card: &card})
			}
			if _, e := tx.ExecContext(ctx, `UPDATE sessions SET status = 'completed', finished_at = ? WHERE id = ?`, plTS(now), r.ID); e != nil {
				return e
			}
			// Mongo copy: same transaction, so a rollback leaves no event (no-op when sync is off).
			if e := s.syncChapterDone(ctx, tx, r.ChildID, c.ParentID, r.World, skill); e != nil {
				return e
			}
		}
		if stored.Mastered {
			sk := skill
			stored.Rewards = append(stored.Rewards, api.Reward{Type: api.SkillMastered, SkillId: &sk})
		}
		// 6. persist the answer and the profile
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
	if finished && !replay { // a finished chapter may complete accepted challenges (feed module)
		if extra, e := s.settleChallenges(ctx, c); e != nil {
			s.Log.Warn("settle challenges failed", "err", e)
		} else if len(extra) > 0 {
			stored.Rewards = append(stored.Rewards, extra...)
		}
	}
	sess, err := s.sessionByID(ctx, r.ID, lang, false, true)
	if err != nil {
		return nil, err
	}
	out := api.AnswerResult{
		Outcome:       api.Outcome(stored.Outcome),
		CorrectAnswer: answerValue(problem.Reponse),
		Denouement:    api.Narration{Status: api.Pending},
		Session:       sess,
		Rewards:       &stored.Rewards,
	}
	if mv := s.methodVisual(ctx, problem.Visual, lang); mv != nil {
		out.MethodVisual = mv
	}
	return api.AnswerBeat200JSONResponse(out), nil
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
