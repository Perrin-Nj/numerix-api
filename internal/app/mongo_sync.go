package app

// Mongo sync producers and resolver (see docs/MONGO_SYNC.md).
//
// Go for Spring people: producers run INSIDE the game transaction and only insert a tiny outbox row.
// The resolver runs later in the worker goroutine, reads SQLite, and builds the pseudonymous document.
// Nothing here changes what any HTTP endpoint answers: SQLite serves every read.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
)

// syncChapterDone queues the three events a finished chapter can change: the child's bests, the family
// board and the world/skill stats bucket. tx is the caller's transaction, so a rollback drops them too.
func (s *Server) syncChapterDone(ctx context.Context, tx *sql.Tx, childID, parentID, world, skill string) error {
	if !s.Sync.Enabled() { // skip the JSON work when Mongo is off
		return nil
	}
	payload, err := json.Marshal(map[string]string{"world": world, "skill": skill})
	if err != nil {
		return fmt.Errorf("encode stats hint: %w", err)
	}
	return errors.Join(
		s.Sync.Enqueue(ctx, tx, mongosync.KindBests, childID, ""),
		s.Sync.Enqueue(ctx, tx, mongosync.KindFamily, parentID, ""),
		s.Sync.Enqueue(ctx, tx, mongosync.KindStats, world+"|"+skill, string(payload)),
	)
}

// syncChildRemoved queues the events that erase a deleted child from Mongo (the resolver sees the missing
// row and answers with a delete) and refresh the family board without that child.
func (s *Server) syncChildRemoved(ctx context.Context, tx *sql.Tx, childID, parentID string) error {
	return errors.Join(
		s.Sync.Enqueue(ctx, tx, mongosync.KindBests, childID, ""),
		s.Sync.Enqueue(ctx, tx, mongosync.KindFamily, parentID, ""),
	)
}

// syncResolver builds the documents for the worker.
type syncResolver struct {
	s *Server
	p *mongosync.Pseudonymizer
}

// SyncResolver returns the Resolver used by the mongosync worker.
func (s *Server) SyncResolver(p *mongosync.Pseudonymizer) mongosync.Resolver {
	return &syncResolver{s: s, p: p}
}

// Resolve dispatches on the event kind. Unknown kinds are skipped, never retried forever.
func (r *syncResolver) Resolve(ctx context.Context, ev mongosync.Event) (mongosync.Op, error) {
	switch ev.Kind {
	case mongosync.KindBests:
		return r.bests(ctx, ev)
	case mongosync.KindFamily:
		return r.family(ctx, ev)
	case mongosync.KindStats:
		return r.stats(ctx, ev)
	}
	return mongosync.Op{}, mongosync.ErrSkip
}

// bests: personal bests of one child, from the SAME computation the engagement endpoint uses (longest streak)
// plus the chapter total. A missing child (deleted) turns into a delete of its pseudonymous document.
func (r *syncResolver) bests(ctx context.Context, ev mongosync.Event) (mongosync.Op, error) {
	op := mongosync.Op{Kind: mongosync.KindBests, Key: r.p.ID("child", ev.Key)}
	var tz string
	switch err := r.s.Store.R.QueryRowContext(ctx, `SELECT timezone FROM children WHERE id = ?`, ev.Key).Scan(&tz); {
	case errors.Is(err, sql.ErrNoRows):
		op.Delete = true
		return op, nil
	case err != nil:
		return op, fmt.Errorf("load child: %w", err)
	}
	sessions, err := r.s.feedSessions(ctx, ev.Key)
	if err != nil {
		return op, fmt.Errorf("load sessions: %w", err)
	}
	eng := r.s.feedEngagement(ctx, plChild{ID: ev.Key, Timezone: tz}, sessions, eventTime(ev))
	chapters := 0
	for _, x := range sessions {
		if x.Status == "completed" && !x.Finished.IsZero() {
			chapters++
		}
	}
	op.Doc = map[string]any{
		"child":               op.Key,
		"longest_streak_days": eng.Streak.LongestDays,
		"current_streak_days": eng.Streak.CurrentDays,
		"chapters_completed":  chapters,
		"updated_at":          time.Now().UTC(),
	}
	return op, nil
}

// family: this week's chapters per sibling under one parent. Names are NOT stored: the family board is read
// back with pseudonyms and the display names are resolved from SQLite at read time.
func (r *syncResolver) family(ctx context.Context, ev mongosync.Event) (mongosync.Op, error) {
	familyID := r.p.ID("family", ev.Key)
	erase := mongosync.Op{Kind: mongosync.KindFamily, Key: familyID, Delete: true, DeleteBy: "family"} // all weeks
	var tz string
	var deleted sql.NullString
	switch err := r.s.Store.R.QueryRowContext(ctx, `SELECT timezone, deleted_at FROM parents WHERE id = ?`, ev.Key).Scan(&tz, &deleted); {
	case errors.Is(err, sql.ErrNoRows):
		return erase, nil
	case err != nil:
		return mongosync.Op{}, fmt.Errorf("load parent: %w", err)
	}
	if deleted.Valid { // account deleted (soft): erase the family's board
		return erase, nil
	}
	loc, lerr := time.LoadLocation(tz)
	if lerr != nil {
		loc = time.UTC
	}
	at := eventTime(ev).In(loc)
	back := (int(at.Weekday()) + 6) % 7 // days since Monday
	start := time.Date(at.Year(), at.Month(), at.Day()-back, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 7)

	rows, err := r.s.Store.R.QueryContext(ctx, `SELECT id FROM children WHERE parent_id = ? ORDER BY id`, ev.Key)
	if err != nil {
		return mongosync.Op{}, fmt.Errorf("list children: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return mongosync.Op{}, fmt.Errorf("scan child: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return mongosync.Op{}, fmt.Errorf("read children: %w", err)
	}
	if len(ids) == 0 {
		return erase, nil // no child left: nothing to show
	}
	members, total := make([]map[string]any, 0, len(ids)), 0
	for _, id := range ids {
		sessions, err := r.s.feedSessions(ctx, id)
		if err != nil {
			return mongosync.Op{}, fmt.Errorf("load sessions: %w", err)
		}
		chapters, days := 0, map[string]bool{}
		for _, x := range sessions {
			if x.Status == "completed" && !x.Finished.Before(start) && x.Finished.Before(end) {
				chapters++
				days[feedDay(x.Finished, loc)] = true
			}
		}
		total += chapters
		members = append(members, map[string]any{"child": r.p.ID("child", id), "chapters": chapters, "days_played": len(days)})
	}
	week := start.Format("2006-01-02")
	return mongosync.Op{Kind: mongosync.KindFamily, Key: familyID + "|" + week, Doc: map[string]any{
		"family": familyID, "week_start": week, "members": members, "total_chapters": total, "updated_at": time.Now().UTC(),
	}}, nil
}

// stats: attempts and correct rate for one world/skill, over all children, only when at least
// mongosync.MinChildren distinct children contributed (k-anonymity). Below the floor the bucket is deleted.
func (r *syncResolver) stats(ctx context.Context, ev mongosync.Event) (mongosync.Op, error) {
	var hint struct{ World, Skill string }
	if err := json.Unmarshal([]byte(ev.Payload), &hint); err != nil || hint.World == "" || hint.Skill == "" {
		return mongosync.Op{}, mongosync.ErrSkip // unreadable hint can never succeed: drop it
	}
	var children, attempts, correct int
	err := r.s.Store.R.QueryRowContext(ctx, `SELECT COUNT(DISTINCT s.child_id), COUNT(*), COALESCE(SUM(a.outcome = 'correct'), 0)
		FROM answers a JOIN sessions s ON s.id = a.session_id
		WHERE s.world_id = ? AND json_extract(a.json, '$.skill_id') = ?`, hint.World, hint.Skill).Scan(&children, &attempts, &correct)
	if err != nil {
		return mongosync.Op{}, fmt.Errorf("aggregate stats: %w", err)
	}
	return statsOp(hint.World, hint.Skill, children, attempts, correct), nil
}

// statsOp applies the k-anonymity floor (pure function, unit tested).
func statsOp(world, skill string, children, attempts, correct int) mongosync.Op {
	op := mongosync.Op{Kind: mongosync.KindStats, Key: world + "|" + skill}
	if children < mongosync.MinChildren {
		op.Delete = true // never publish a small bucket, and remove one that shrank (a child was deleted)
		return op
	}
	op.Doc = map[string]any{
		"world": world, "skill": skill, "children": children, "attempts": attempts,
		"correct_rate": float64(correct) / float64(attempts), "updated_at": time.Now().UTC(),
	}
	return op
}

// eventTime is when the game change happened (the outbox row's creation time), or now if unknown.
func eventTime(ev mongosync.Event) time.Time {
	if ev.CreatedAt.IsZero() {
		return time.Now().UTC()
	}
	return ev.CreatedAt
}
