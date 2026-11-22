package app

// Second try (plan 3.3). The first answer is stored in `answers` and is the only one mastery sees. The second
// try is stored in `answer_retries` (one row per beat), never calls Observe, and returns the full reveal. A
// correct second try counts half in the card threshold.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/narrator"
)

// retryRecord is the JSON kept in answer_retries.json: enough to replay the same reveal.
type retryRecord struct {
	Outcome  string          `json:"outcome"`
	Code     string          `json:"code_erreur,omitempty"`
	GivenRaw json.RawMessage `json:"given_raw,omitempty"`
	Rewards  []api.Reward    `json:"rewards"`
}

func (s *Server) answerRetry(ctx context.Context, r sessRow, c plChild, req api.AnswerBeatRequestObject, lang string, retryOn bool) (api.AnswerBeatResponseObject, error) {
	beatID, idem := plUID(req.BeatId), plUID(req.Params.IdempotencyKey)
	rawGiven, _ := req.Body.Answer.MarshalJSON()
	given, _ := engineValue(req.Body.Answer) // already validated by the caller
	var first storedAnswer
	var rec retryRecord
	var problem engine.Problem
	var cur *content.Question
	var curRes content.Result
	var finishedNow bool
	notAllowed := func() error { return plConflict("retry_not_allowed", "A second try is not allowed for this question.") }
	err := s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var qid string
		var idx int
		if e := tx.QueryRowContext(ctx, `SELECT question_id, idx FROM beats WHERE id = ? AND session_id = ?`, beatID, r.ID).Scan(&qid, &idx); errors.Is(e, sql.ErrNoRows) {
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
		// the same key replays the stored reveal, another key is a conflict
		var prevKey, raw string
		switch e := tx.QueryRowContext(ctx, `SELECT idempotency_key, json FROM answer_retries WHERE beat_id = ?`, beatID).Scan(&prevKey, &raw); {
		case e == nil:
			if prevKey != idem {
				return plConflict("beat_answered", "This question was already answered.")
			}
			if e := json.Unmarshal([]byte(raw), &rec); e != nil {
				return e
			}
			if cur != nil {
				curRes = cur.Check(rec.GivenRaw)
			}
			return e
		case !errors.Is(e, sql.ErrNoRows):
			return e
		}
		// a first answer must exist, be wrong, and have been given with the second try open
		var fj string
		if e := tx.QueryRowContext(ctx, `SELECT json FROM answers WHERE beat_id = ? LIMIT 1`, beatID).Scan(&fj); errors.Is(e, sql.ErrNoRows) {
			return notAllowed()
		} else if e != nil {
			return e
		}
		if e := json.Unmarshal([]byte(fj), &first); e != nil {
			return e
		}
		if !retryOn || !first.RetryOpen {
			return notAllowed()
		}
		var status string
		if e := tx.QueryRowContext(ctx, `SELECT status FROM sessions WHERE id = ?`, r.ID).Scan(&status); e != nil {
			return e
		}
		switch status {
		case "paused":
			return plConflict("session_paused", "This chapter is paused.")
		case "abandoned":
			return plConflict("session_completed", "This chapter is already complete.")
		}
		var later int // the second try belongs to the latest answered question only
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM answers a JOIN beats b ON b.id = a.beat_id WHERE a.session_id = ? AND b.idx > ?`, r.ID, idx).Scan(&later); e != nil {
			return e
		}
		if later > 0 {
			return notAllowed()
		}
		var res engine.Result
		if cur != nil {
			curRes = cur.Check(rawGiven)
			res = engine.Result{Type: engine.Outcome(curRes.Outcome), ReponseDonnee: given, CompetenceID: cur.Skill, CodeErreur: curRes.Misconception}
		} else {
			res = problem.Evaluate(given)
		}
		rec = retryRecord{Outcome: string(res.Type), Code: res.CodeErreur, GivenRaw: rawGiven, Rewards: append([]api.Reward{}, first.Rewards...)}
		now := s.game().now()
		rj, e := json.Marshal(rec)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO answer_retries (beat_id, session_id, idempotency_key, outcome, json, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			beatID, r.ID, idem, rec.Outcome, string(rj), plTS(now)); e != nil {
			return e
		}
		if first.PendingFinish { // the wrong answer was the last of the chapter: it closes now, with half credit for a right second try
			rw, e := s.finishChapter(ctx, tx, r, c, now, first.Skill, 0, 0)
			if e != nil {
				return e
			}
			rec.Rewards = append(rec.Rewards, rw...)
			finishedNow = true
			if rj, e = json.Marshal(rec); e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `UPDATE answer_retries SET json = ? WHERE beat_id = ?`, string(rj), beatID)
			return e
		}
		return nil
	})
	var ae *httpapi.APIError
	if errors.As(err, &ae) {
		return nil, err
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "answer retry", err)
	}
	if finishedNow { // the chapter closed with this second try: accepted challenges may complete (feed module)
		if extra, e := s.settleChallenges(ctx, c); e != nil {
			s.Log.Warn("settle challenges failed", "err", e)
		} else if len(extra) > 0 {
			rec.Rewards = append(rec.Rewards, extra...)
		}
	}
	sess, err := s.sessionByID(ctx, r.ID, lang, false, true)
	if err != nil {
		return nil, err
	}
	two := api.AnswerResultAttemptN2
	out := s.revealResult(ctx, sess, storedAnswer{Outcome: rec.Outcome, Code: rec.Code, Skill: problem.CompetenceID, Rewards: rec.Rewards}, problem, cur, curRes, lang)
	out.Attempt = &two
	return api.AnswerBeat200JSONResponse(out), nil
}

// retryNudge is the last-resort second-try hint: a calm sentence per question type, with no number in it.
func retryNudge(qtype, lang string) string {
	fr := map[string]string{
		"numeric": "Relis l’énoncé et refais le calcul étape par étape.",
		"choice":  "Relis chaque choix avec calme, puis essaie encore.",
		"order":   "Regarde le premier et le dernier, puis range le reste.",
		"match":   "Cherche d’abord le lien le plus facile, puis continue.",
	}
	en := map[string]string{
		"numeric": "Read the story again and redo the work one step at a time.",
		"choice":  "Read each choice calmly, then try again.",
		"order":   "Look at the first and the last one, then place the rest.",
		"match":   "Find the easiest pair first, then keep going.",
	}
	m, def := fr, "Relis l’énoncé et essaie encore, tu peux y arriver."
	if lang == "en" || lang == "en-CA" {
		m, def = en, "Read the question again and give it another try, you can do it."
	}
	if t, ok := m[qtype]; ok {
		return t
	}
	return def
}

// retryHintText picks the second-try hint: the message written for the typical error that was made, else the
// question's retry hint, else its hint, else a nudge for the question type. A candidate that gives the answer away
// is skipped, so the result is always safe to show.
func retryHintText(cq *content.Question, givenRaw json.RawMessage, lang string) string {
	pick := []string{cq.Check(givenRaw).Feedback.Get(lang)}
	if cq.RetryHint != nil {
		pick = append(pick, cq.RetryHint.Get(lang))
	}
	pick = append(pick, cq.Hint.Get(lang))
	for _, cand := range pick {
		if cand != "" && !cq.Leaks(cand, lang) {
			return cand
		}
	}
	return retryNudge(cq.Type, lang)
}

// finishChapter closes the chapter: counts the first-try score (plus half a point for each correct second try),
// awards the card once when the threshold is met, marks the session completed and queues the Mongo copy. extraAnswered
// and extraCorrect count an answer of the caller's transaction that is not in the table yet.
func (s *Server) finishChapter(ctx context.Context, tx *sql.Tx, r sessRow, c plChild, now time.Time, skill string, extraAnswered, extraCorrect int) ([]api.Reward, error) {
	var answered, correct, retried int
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(outcome = 'correct'), 0) FROM answers WHERE session_id = ?`, r.ID).Scan(&answered, &correct); e != nil {
		return nil, e
	}
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM answer_retries WHERE session_id = ? AND outcome = 'correct'`, r.ID).Scan(&retried); e != nil {
		return nil, e
	}
	answered, correct = answered+extraAnswered, correct+extraCorrect
	rewards := []api.Reward{{Type: api.ChapterComplete}}
	pct := (float64(correct) + 0.5*float64(retried)) / float64(max(answered, 1)) * 100
	if pct >= float64(s.plConfig(ctx).CardThresholdPct) {
		sid := plUUID(r.ID)
		card := api.StoryCard{Id: plUUID(newID()), World: api.WorldId(r.World), Title: narrator.TitleOf(r.World, r.Data.TitleIdx, r.Data.Lang),
			ImageUrl: "", SessionId: &sid, EarnedAt: now}
		cj, _ := json.Marshal(map[string]string{"world": r.World, "title": card.Title, "session_id": r.ID, "image_url": ""})
		if _, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO collection (child_id, card_id, json, acquired_at) VALUES (?, ?, ?, ?)`,
			r.ChildID, plUID(card.Id), string(cj), plTS(now)); e != nil {
			return nil, e
		}
		rewards = append(rewards, api.Reward{Type: api.CardEarned, Card: &card})
	}
	if _, e := tx.ExecContext(ctx, `UPDATE sessions SET status = 'completed', finished_at = ? WHERE id = ? AND status <> 'completed'`, plTS(now), r.ID); e != nil {
		return nil, e
	}
	// Mongo copy: same transaction, so a rollback leaves no event (no-op when sync is off).
	if e := s.syncChapterDone(ctx, tx, r.ChildID, c.ParentID, r.World, skill); e != nil {
		return nil, e
	}
	return rewards, nil
}

// closeRetryWindow ends a second try that is still open when the child asks for the next beat: the window is marked
// skipped and, when the wrong answer was the last of the chapter, the chapter is finished with the first-try score.
// It reports whether the chapter was completed.
func (s *Server) closeRetryWindow(ctx context.Context, tx *sql.Tx, r sessRow, c plChild, now time.Time) (bool, error) {
	var beatID, aj string
	err := tx.QueryRowContext(ctx, `SELECT a.beat_id, a.json FROM answers a JOIN beats b ON b.id = a.beat_id
		WHERE a.session_id = ? AND json_extract(a.json, '$.retry_open') = 1
		  AND NOT EXISTS (SELECT 1 FROM answer_retries t WHERE t.beat_id = a.beat_id)
		ORDER BY b.idx DESC LIMIT 1`, r.ID).Scan(&beatID, &aj)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO answer_retries (beat_id, session_id, idempotency_key, outcome, json, created_at) VALUES (?, ?, 'skipped', 'skipped', '{}', ?)`,
		beatID, r.ID, plTS(now)); e != nil {
		return false, e
	}
	var st storedAnswer
	_ = json.Unmarshal([]byte(aj), &st)
	if !st.PendingFinish {
		return false, nil
	}
	if _, e := s.finishChapter(ctx, tx, r, c, now, st.Skill, 0, 0); e != nil {
		return false, e
	}
	return true, nil
}

// retryBeatFrom returns the beat whose second try is open (a wrong first answer given with attempt 1, no second
// answer and no skip yet). Only the latest answered beat can be in that state: asking for the next beat closes it.
func retryBeatFrom(ctx context.Context, q rowQuerier, sessionID string) (beatRow, bool, error) {
	var b beatRow
	var qid sql.NullString
	err := q.QueryRowContext(ctx, `SELECT b.id, b.session_id, b.idx, b.question_id, COALESCE(json_extract(b.json,'$.shape'),''), b.created_at
		FROM beats b JOIN answers a ON a.beat_id = b.id
		WHERE b.session_id = ? AND json_extract(a.json, '$.retry_open') = 1
		  AND NOT EXISTS (SELECT 1 FROM answer_retries t WHERE t.beat_id = b.id)
		ORDER BY b.idx DESC LIMIT 1`, sessionID).Scan(&b.ID, &b.SessionID, &b.Index, &qid, &b.Shape, &b.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return beatRow{}, false, nil
	}
	b.QuestionID = qid.String
	return b, err == nil, err
}
