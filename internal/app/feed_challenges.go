// Challenges and the welcome gift: listChallenges, acceptChallenge, claimWelcomeGift.
//
// Go for Spring people: a challenge is catalogue data (feedChallengeDefs, in code) plus one row per child in the
// existing `challenges` table (id "<childId>:<challengeId>", json {"challenge_id","accepted_at","completed_at"}).
// Progress is never stored: it is recomputed from game events that happened after accepted_at, so it cannot drift.
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// feedChallengeDef is one entry of the challenge catalogue (BACKEND_INTEGRATION section 14).
type feedChallengeDef struct {
	id, emoji, metric string
	target            int
	title, desc       [2]string // [fr, en]
	reward, cardTitle [2]string
	world             api.WorldId // world of the reward card
}

// feedChallengeDefs mirrors CHALLENGES in frontend/src/domain/game/catalog.ts.
var feedChallengeDefs = []feedChallengeDef{
	{"first_steps", "👣", "chapters_done", 1, [2]string{"Premiers pas", "First steps"}, [2]string{"Termine un chapitre jusqu’au bout.", "Finish one chapter all the way."}, [2]string{"Une carte de héros", "A hero card"}, [2]string{"Le premier pas", "The first step"}, "nature"},
	{"sharp_three", "🎯", "correct_streak", 3, [2]string{"Triplé gagnant", "Winning hat trick"}, [2]string{"Réponds juste 3 fois de suite.", "Answer right 3 times in a row."}, [2]string{"Une carte rare", "A rare card"}, [2]string{"Le tir parfait", "The perfect shot"}, "sport"},
	{"world_tourist", "🧭", "worlds_played", 3, [2]string{"Touriste des mondes", "World tourist"}, [2]string{"Joue dans 3 mondes différents.", "Play in 3 different worlds."}, [2]string{"Une carte voyageuse", "A traveler card"}, [2]string{"Le carnet de voyage", "The travel journal"}, "espace"},
	{"story_marathon", "📚", "chapters_done", 3, [2]string{"Marathon d’histoires", "Story marathon"}, [2]string{"Termine 3 chapitres.", "Finish 3 chapters."}, [2]string{"Une carte brillante", "A shiny card"}, [2]string{"La grande bibliothèque", "The great library"}, "science"},
	{"feed_fan", "💛", "likes", 5, [2]string{"Curieux de tout", "Curious about everything"}, [2]string{"Aime 5 cartes dans Découvrir.", "Like 5 cards in Discover."}, [2]string{"Une carte cœur", "A heart card"}, [2]string{"Le coin des curieux", "The curious corner"}, "cuisine"},
	{"puzzle_pro", "🧩", "puzzles_correct", 3, [2]string{"Pro des casse-têtes", "Puzzle pro"}, [2]string{"Résous 3 casse-têtes de Découvrir.", "Solve 3 puzzles in Discover."}, [2]string{"Une carte mystère", "A mystery card"}, [2]string{"La clé du mystère", "The mystery key"}, "echecs"},
	{"daily_hello", "☀️", "days_played", 2, [2]string{"Retour de flamme", "Welcome back"}, [2]string{"Joue deux jours différents.", "Play on two different days."}, [2]string{"Une carte soleil", "A sun card"}, [2]string{"Le jour qui revient", "The day that returns"}, "medecine"},
}

// feedChallengeByCard finds the definition behind a card id such as "ch-sharp_three".
func feedChallengeByCard(cardID string) (feedChallengeDef, bool) {
	if !strings.HasPrefix(cardID, "ch-") {
		return feedChallengeDef{}, false
	}
	return feedChallengeByID(strings.TrimPrefix(cardID, "ch-"))
}

// feedChallengeByID finds a definition by challenge id.
func feedChallengeByID(id string) (feedChallengeDef, bool) {
	for _, d := range feedChallengeDefs {
		if d.id == id {
			return d, true
		}
	}
	return feedChallengeDef{}, false
}

// feedChalRow is the stored state of one accepted challenge.
type feedChalRow struct {
	Accepted time.Time
	Done     bool
}

// feedChalRows loads the child's accepted challenges, keyed by challenge id.
func (s *Server) feedChalRows(ctx context.Context, childID string) (map[string]feedChalRow, error) {
	rows, err := s.Store.R.QueryContext(ctx, `SELECT status, json FROM challenges WHERE child_id = ?`, childID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]feedChalRow{}
	for rows.Next() {
		var status, raw string
		if err := rows.Scan(&status, &raw); err != nil {
			return nil, err
		}
		var j struct {
			ID       string `json:"challenge_id"`
			Accepted string `json:"accepted_at"`
		}
		if json.Unmarshal([]byte(raw), &j) == nil && j.ID != "" {
			out[j.ID] = feedChalRow{Accepted: feedTime(j.Accepted), Done: status == "done"}
		}
	}
	return out, rows.Err()
}

// feedEvents is the game history the metrics read: only events, no totals.
type feedEvents struct {
	sessions []feedSession
	answers  []feedAnswer // chapter answers in order
	likes    []time.Time  // start of every current like
	puzzles  []time.Time  // time of every correct feed puzzle
}

// feedAnswer is one chapter answer.
type feedAnswer struct {
	outcome string
	at      time.Time
}

// feedLoadEvents reads sessions, answers, likes and puzzle results of a child (read-only).
func (s *Server) feedLoadEvents(ctx context.Context, childID string) (feedEvents, error) {
	var ev feedEvents
	var err error
	if ev.sessions, err = s.feedSessions(ctx, childID); err != nil {
		return ev, err
	}
	// Run a query and hand every (text, text) row to add.
	each := func(q string, add func(a, b string)) error {
		rows, err := s.Store.R.QueryContext(ctx, q, childID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a, b string
			if err := rows.Scan(&a, &b); err != nil {
				return err
			}
			add(a, b)
		}
		return rows.Err()
	}
	if err = each(`SELECT COALESCE(a.outcome,''), a.created_at FROM answers a JOIN sessions s ON s.id = a.session_id
		WHERE s.child_id = ? ORDER BY a.created_at, a.rowid`, func(o, t string) { ev.answers = append(ev.answers, feedAnswer{o, feedTime(t)}) }); err != nil {
		return ev, err
	}
	if err = each(`SELECT 'l', liked_at FROM feed_reactions WHERE child_id = ? AND liked = 1 AND liked_at IS NOT NULL`,
		func(_, t string) { ev.likes = append(ev.likes, feedTime(t)) }); err != nil {
		return ev, err
	}
	err = each(`SELECT 'p', created_at FROM feed_answers WHERE child_id = ? AND outcome = 'correct'`,
		func(_, t string) { ev.puzzles = append(ev.puzzles, feedTime(t)) })
	return ev, err
}

// feedProgress counts the events of one metric that happened strictly after since (capped by the caller).
func feedProgress(metric string, ev feedEvents, since time.Time, loc *time.Location) int {
	switch metric {
	case "chapters_done":
		n := 0
		for _, x := range ev.sessions {
			if x.Status == "completed" && x.Finished.After(since) {
				n++
			}
		}
		return n
	case "days_played":
		days := map[string]bool{}
		for _, x := range ev.sessions {
			if x.Status == "completed" && x.Finished.After(since) {
				days[feedDay(x.Finished, loc)] = true
			}
		}
		return len(days)
	case "worlds_played":
		worlds := map[string]bool{}
		for _, x := range ev.sessions {
			if x.World != "" && x.Started.After(since) {
				worlds[x.World] = true
			}
		}
		return len(worlds)
	case "correct_streak": // longest run of right answers in a row
		best, run := 0, 0
		for _, a := range ev.answers {
			if !a.at.After(since) {
				continue
			}
			if a.outcome == "correct" {
				run++
				best = max(best, run)
			} else {
				run = 0
			}
		}
		return best
	case "likes":
		return feedCountAfter(ev.likes, since)
	case "puzzles_correct":
		return feedCountAfter(ev.puzzles, since)
	}
	return 0
}

// feedCountAfter counts the times strictly after since.
func feedCountAfter(ts []time.Time, since time.Time) int {
	n := 0
	for _, t := range ts {
		if t.After(since) {
			n++
		}
	}
	return n
}

// feedChallengeViews builds the API view of every challenge: active first, then available, then done.
func (s *Server) feedChallengeViews(ctx context.Context, c plChild, en bool) ([]api.Challenge, error) {
	rows, err := s.feedChalRows(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	ev, err := s.feedLoadEvents(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	cards, err := s.feedCards(ctx, c, en, nil, 0)
	if err != nil {
		return nil, err
	}
	l := feedLang(en)
	out := make([]api.Challenge, 0, len(feedChallengeDefs))
	for _, d := range feedChallengeDefs {
		v := api.Challenge{Id: d.id, Title: d.title[l], Description: d.desc[l], Target: d.target, Status: "available", RewardLabel: d.reward[l], Emoji: d.emoji}
		if r, ok := rows[d.id]; ok {
			at := r.Accepted
			v.AcceptedAt = &at
			v.Status = "active"
			v.Progress = min(d.target, feedProgress(d.metric, ev, r.Accepted, feedLoc(c)))
			if r.Done {
				v.Status, v.Progress = "done", d.target
				want := plUUID(feedCardFrom("ch-"+d.id, "{}", time.Time{}, en).Id.String()) // same deterministic id
				for i := range cards {
					if cards[i].Id == want {
						v.Card = &cards[i]
					}
				}
			}
		}
		out = append(out, v)
	}
	rank := map[api.ChallengeStatus]int{"active": 0, "available": 1, "done": 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Status] < rank[out[j].Status] })
	return out, nil
}

// settleChallenges completes every accepted challenge whose target is reached, awarding its card once.
// It returns one card_earned reward per challenge completed by THIS call, so the action that caused it can
// show the celebration. Other modules call it after a chapter ends (the last answer of a chapter).
func (s *Server) settleChallenges(ctx context.Context, c plChild) ([]api.Reward, error) {
	rows, err := s.feedChalRows(ctx, c.ID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load challenges", err)
	}
	pending := false // anything accepted and not done?
	for _, r := range rows {
		pending = pending || !r.Done
	}
	if !pending {
		return nil, nil
	}
	ev, err := s.feedLoadEvents(ctx, c.ID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load events", err)
	}
	var rewards []api.Reward
	en := plLocaleOf(nil, c.Locale) == "en-CA"
	for _, d := range feedChallengeDefs {
		r, ok := rows[d.id]
		if !ok || r.Done || feedProgress(d.metric, ev, r.Accepted, feedLoc(c)) < d.target {
			continue
		}
		now := time.Now().UTC().Format(plTSLayout)
		won := false // true when this call flipped the challenge to done
		err := s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
			res, e := tx.ExecContext(ctx, `UPDATE challenges SET status = 'done', json = json_set(json, '$.completed_at', ?)
				WHERE id = ? AND status <> 'done'`, now, c.ID+":"+d.id)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if won = n == 1; !won { // someone else completed it first: no second card
				return nil
			}
			_, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO collection (child_id, card_id, json, acquired_at) VALUES (?, ?, ?, ?)`,
				c.ID, "ch-"+d.id, `{"world":"`+string(d.world)+`"}`, now)
			return e
		})
		if err != nil {
			return nil, s.plInternalErr(ctx, "complete challenge", err)
		}
		if won {
			card := feedCardFrom("ch-"+d.id, "{}", feedTime(now), en)
			rewards = append(rewards, api.Reward{Type: api.CardEarned, Card: &card})
		}
	}
	return rewards, nil
}

// ListChallenges answers GET /children/{childId}/challenges.
func (s *Server) ListChallenges(ctx context.Context, req api.ListChallengesRequestObject) (api.ListChallengesResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.plOwnedChild(ctx, p, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	if _, err := s.settleChallenges(ctx, c); err != nil { // finish what is already reached before listing
		return nil, err
	}
	items, err := s.feedChallengeViews(ctx, c, plLocaleOf(req.Params.AcceptLanguage, c.Locale) == "en-CA")
	if err != nil {
		return nil, s.plInternalErr(ctx, "list challenges", err)
	}
	return api.ListChallenges200JSONResponse{Items: items}, nil
}

// AcceptChallenge answers POST /children/{childId}/challenges/{challengeId}/accept. Idempotent.
func (s *Server) AcceptChallenge(ctx context.Context, req api.AcceptChallengeRequestObject) (api.AcceptChallengeResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.plOwnedChild(ctx, p, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	if _, ok := feedChallengeByID(req.ChallengeId); !ok {
		return nil, httpapi.ErrNotFound
	}
	now := time.Now().UTC().Format(plTSLayout)
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		// INSERT OR IGNORE keeps the first accepted_at when accepted twice.
		_, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO challenges (id, child_id, status, json, created_at) VALUES (?, ?, 'active', ?, ?)`,
			c.ID+":"+req.ChallengeId, c.ID, `{"challenge_id":"`+req.ChallengeId+`","accepted_at":"`+now+`"}`, now)
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "accept challenge", err)
	}
	items, err := s.feedChallengeViews(ctx, c, plLocaleOf(req.Params.AcceptLanguage, c.Locale) == "en-CA")
	if err != nil {
		return nil, s.plInternalErr(ctx, "load challenge", err)
	}
	for _, v := range items {
		if v.Id == req.ChallengeId {
			return api.AcceptChallenge200JSONResponse(v), nil
		}
	}
	return nil, httpapi.ErrNotFound
}

// ClaimWelcomeGift answers POST /children/{childId}/gift/claim: 201 the first time, 200 afterwards, 404 when the feature is off.
func (s *Server) ClaimWelcomeGift(ctx context.Context, req api.ClaimWelcomeGiftRequestObject) (api.ClaimWelcomeGiftResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.plOwnedChild(ctx, p, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	if !s.Config.FeatureWelcomeGift { // FEATURE_WELCOME_GIFT is off: no welcome gift exists
		return nil, httpapi.NewError(http.StatusNotFound, "not_found", "")
	}
	now := time.Now().UTC().Format(plTSLayout)
	created := false
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		res, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO collection (child_id, card_id, json, acquired_at) VALUES (?, 'welcome', '{"world":"nature"}', ?)`, c.ID, now)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if created = n == 1; !created { // already claimed
			return nil
		}
		_, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO gifts (id, child_id, status, json, claimed_at, created_at) VALUES (?, ?, 'claimed', '{"kind":"welcome"}', ?, ?)`,
			c.ID+":welcome", c.ID, now, now)
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "claim gift", err)
	}
	var at string
	if err := s.Store.R.QueryRowContext(ctx, `SELECT acquired_at FROM collection WHERE child_id = ? AND card_id = 'welcome'`, c.ID).Scan(&at); err != nil {
		return nil, s.plInternalErr(ctx, "load gift card", err)
	}
	card := feedCardFrom("welcome", `{"world":"nature"}`, feedTime(at), plLocaleOf(req.Params.AcceptLanguage, c.Locale) == "en-CA")
	if created {
		return api.ClaimWelcomeGift201JSONResponse(card), nil
	}
	return api.ClaimWelcomeGift200JSONResponse(card), nil
}
