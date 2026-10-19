package contentdb

// Mongo documents for the content collections (docs/MONGO_SYNC.md). The outbox rows only carry a key; these
// functions read the CURRENT state from SQLite when the worker asks, so a replay always sends the latest text.
// Content is not personal data. The statistics collection is aggregated over children and is written only when at
// least mongosync.MinChildren distinct children contributed (k-anonymity), and it never holds a child id.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
)

// ResolveEvent builds the Mongo operation for a content event. ok is false for a kind that is not a content kind.
func (r *Repo) ResolveEvent(ctx context.Context, ev mongosync.Event) (op mongosync.Op, ok bool, err error) {
	switch ev.Kind {
	case mongosync.KindQuestionBank:
		op, err = r.resolveBank(ctx, ev.Key)
	case mongosync.KindQuestionAudit:
		op, err = r.resolveAudit(ctx, ev.Key)
	case mongosync.KindQuestionStats:
		op, err = r.resolveStats(ctx, ev.Key)
	case mongosync.KindContentWorld:
		op, err = r.resolveWorld(ctx, ev.Key)
	case mongosync.KindContentQuest:
		op, err = r.resolveQuest(ctx, ev.Key)
	default:
		return mongosync.Op{}, false, nil
	}
	return op, true, err
}

// resolveBank sends the whole current question object plus its indexed columns.
func (r *Repo) resolveBank(ctx context.Context, id string) (mongosync.Op, error) {
	op := mongosync.Op{Kind: mongosync.KindQuestionBank, Key: id}
	row, err := r.Get(ctx, r.DB.R, id)
	if errors.Is(err, ErrNotFound) {
		op.Delete = true
		return op, nil
	}
	if err != nil {
		return op, err
	}
	var body map[string]any
	var raw string
	if err := r.DB.R.QueryRowContext(ctx, `SELECT content_json FROM bank_questions WHERE id = ?`, id).Scan(&raw); err != nil {
		return op, fmt.Errorf("read question json: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return op, fmt.Errorf("decode question json: %w", err)
	}
	op.Doc = map[string]any{
		"world": row.WorldID, "quest": row.Q.Quest, "skill": row.Q.Skill, "type": row.Q.Type, "grade": row.Q.Grade, "difficulty": row.Q.Difficulty,
		"status": row.Q.Status, "version": row.Q.Version, "content_hash": row.Hash, "author": row.Q.Author, "source": row.Q.Source,
		"ai_drafted": row.Q.AIDrafted, "reviewed_by": row.Q.ReviewedBy, "updated_at": row.UpdatedAt, "published_at": row.PublishedAt, "question": body,
	}
	return op, nil
}

// resolveAudit sends one audit row.
func (r *Repo) resolveAudit(ctx context.Context, id string) (mongosync.Op, error) {
	op := mongosync.Op{Kind: mongosync.KindQuestionAudit, Key: id}
	var at, actor, action, qid, hash, detail string
	var from, to sql.NullInt64
	err := r.DB.R.QueryRowContext(ctx, `SELECT at, actor, action, question_id, from_version, to_version, content_hash, detail FROM question_audit WHERE id = ?`, id).
		Scan(&at, &actor, &action, &qid, &from, &to, &hash, &detail)
	if errors.Is(err, sql.ErrNoRows) {
		return op, mongosync.ErrSkip // audit rows are never deleted; a missing one cannot be sent
	}
	if err != nil {
		return op, fmt.Errorf("read question audit: %w", err)
	}
	op.Doc = map[string]any{"at": at, "actor": actor, "action": action, "question_id": qid, "from_version": nullInt(from), "to_version": nullInt(to),
		"content_hash": hash, "detail": detail}
	return op, nil
}

func nullInt(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}

// resolveWorld sends a world.
func (r *Repo) resolveWorld(ctx context.Context, id string) (mongosync.Op, error) {
	op := mongosync.Op{Kind: mongosync.KindContentWorld, Key: id}
	rows, err := r.Worlds(ctx, r.DB.R)
	if err != nil {
		return op, err
	}
	i := slices.IndexFunc(rows, func(w WorldRow) bool { return w.ID == id })
	if i < 0 {
		op.Delete = true
		return op, nil
	}
	w := rows[i]
	op.Doc = map[string]any{"ord": w.Ord, "name": map[string]string{"fr": w.Name.FR, "en": w.Name.EN}, "tagline": map[string]string{"fr": w.Tagline.FR, "en": w.Tagline.EN},
		"hook": map[string]string{"fr": w.Hook.FR, "en": w.Hook.EN}, "accent_color": w.AccentColor, "grade_min": w.GradeMin, "grade_max": w.GradeMax,
		"strand": w.Strand, "hero_asset_id": w.HeroAssetID, "status": w.Status, "content_hash": w.Hash}
	return op, nil
}

// resolveQuest sends a quest.
func (r *Repo) resolveQuest(ctx context.Context, id string) (mongosync.Op, error) {
	op := mongosync.Op{Kind: mongosync.KindContentQuest, Key: id}
	rows, err := r.Quests(ctx, r.DB.R, "")
	if err != nil {
		return op, err
	}
	i := slices.IndexFunc(rows, func(q QuestRow) bool { return q.ID == id })
	if i < 0 {
		op.Delete = true
		return op, nil
	}
	q := rows[i]
	op.Doc = map[string]any{"world": q.World, "ord": q.Ord, "skill": q.Skill, "name": map[string]string{"fr": q.Name.FR, "en": q.Name.EN},
		"goal": map[string]string{"fr": q.Goal.FR, "en": q.Goal.EN}, "grade_min": q.GradeMin, "grade_max": q.GradeMax, "target_count": q.TargetCount,
		"icon_asset_id": q.IconAssetID, "status": q.Status, "content_hash": q.Hash}
	return op, nil
}

// StatsOp builds the statistics document from numbers already counted (pure, unit tested for the k-anonymity floor).
func StatsOp(id string, children, attempts, correct, typical int, top []map[string]any, medianMs int, meta map[string]any) mongosync.Op {
	op := mongosync.Op{Kind: mongosync.KindQuestionStats, Key: id}
	if children < mongosync.MinChildren || attempts == 0 {
		op.Delete = true // too few children: never publish, and remove a document that shrank
		return op
	}
	doc := map[string]any{"attempts": attempts, "children": children, "correct_rate": float64(correct) / float64(attempts),
		"typical_error_rate": float64(typical) / float64(attempts), "top_misconceptions": top, "median_response_ms": medianMs, "updated_at": time.Now().UTC()}
	for k, v := range meta {
		doc[k] = v
	}
	op.Doc = doc
	return op
}

// resolveStats aggregates the answers to one bank question over all children.
func (r *Repo) resolveStats(ctx context.Context, id string) (mongosync.Op, error) {
	var world, quest, skill string
	err := r.DB.R.QueryRowContext(ctx, `SELECT world_id, quest_id, skill_id FROM bank_questions WHERE id = ?`, id).Scan(&world, &quest, &skill)
	if errors.Is(err, sql.ErrNoRows) {
		return mongosync.Op{Kind: mongosync.KindQuestionStats, Key: id, Delete: true}, nil
	}
	if err != nil {
		return mongosync.Op{}, fmt.Errorf("read question: %w", err)
	}
	const from = ` FROM answers a JOIN beats b ON b.id = a.beat_id JOIN questions q ON q.id = b.question_id JOIN sessions s ON s.id = a.session_id WHERE q.bank_id = ?`
	var children, attempts, correct, typical int
	if err := r.DB.R.QueryRowContext(ctx, `SELECT COUNT(DISTINCT s.child_id), COUNT(*), COALESCE(SUM(a.outcome = 'correct'),0), COALESCE(SUM(a.outcome = 'erreur_typique'),0)`+from, id).
		Scan(&children, &attempts, &correct, &typical); err != nil {
		return mongosync.Op{}, fmt.Errorf("aggregate answers: %w", err)
	}
	if children < mongosync.MinChildren {
		return StatsOp(id, children, attempts, correct, typical, nil, 0, nil), nil
	}
	top := []map[string]any{}
	rows, err := r.DB.R.QueryContext(ctx, `SELECT json_extract(a.json,'$.code_erreur'), COUNT(*)`+from+` AND a.outcome = 'erreur_typique' AND json_extract(a.json,'$.code_erreur') IS NOT NULL
		GROUP BY 1 HAVING COUNT(*) >= ? ORDER BY COUNT(*) DESC, 1 LIMIT 5`, id, mongosync.MinChildren)
	if err != nil {
		return mongosync.Op{}, fmt.Errorf("top misconceptions: %w", err)
	}
	for rows.Next() {
		var code string
		var n int
		if err := rows.Scan(&code, &n); err != nil {
			_ = rows.Close()
			return mongosync.Op{}, err
		}
		top = append(top, map[string]any{"code": code, "count": n})
	}
	if err := rows.Close(); err != nil {
		return mongosync.Op{}, err
	}
	var times []int
	mrows, err := r.DB.R.QueryContext(ctx, `SELECT CAST(json_extract(a.json,'$.response_ms') AS INTEGER)`+from+` AND json_extract(a.json,'$.response_ms') > 0`, id)
	if err != nil {
		return mongosync.Op{}, fmt.Errorf("response times: %w", err)
	}
	for mrows.Next() {
		var ms int
		if err := mrows.Scan(&ms); err != nil {
			_ = mrows.Close()
			return mongosync.Op{}, err
		}
		times = append(times, ms)
	}
	if err := mrows.Close(); err != nil {
		return mongosync.Op{}, err
	}
	slices.Sort(times)
	median := 0
	if len(times) > 0 {
		median = times[len(times)/2]
	}
	return StatsOp(id, children, attempts, correct, typical, top, median, map[string]any{"world": world, "quest": quest, "skill": skill}), nil
}
