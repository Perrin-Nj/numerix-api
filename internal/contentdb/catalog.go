package contentdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
)

// Change tells what an upsert did.
type Change int

// The three results of an idempotent upsert.
const (
	Unchanged Change = iota
	Added
	Changed
)

// WorldRow is a stored world with its hero asset.
type WorldRow struct {
	content.World
	HeroAssetID string `json:"hero_asset_id"`
	Hash        string `json:"content_hash"`
}

// QuestRow is a stored quest with its icon asset.
type QuestRow struct {
	content.Quest
	IconAssetID string `json:"icon_asset_id"`
	Hash        string `json:"content_hash"`
}

// resolveAsset turns an asset id or alias into a stored id ("" stays "", an unknown value is an error).
func resolveAsset(ctx context.Context, tx *sql.Tx, ref string) (any, error) {
	if ref == "" {
		return nil, nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM assets WHERE id = ? OR alias = ?`, ref, ref).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("asset %q does not exist", ref)
	}
	if err != nil {
		return nil, err
	}
	return id, nil
}

// UpsertWorld inserts or updates a world when its content hash changed.
func (r *Repo) UpsertWorld(ctx context.Context, tx *sql.Tx, w content.World, actor string) (Change, error) {
	if w.Status == "" {
		w.Status = "published"
	}
	hash, err := content.Hash(w)
	if err != nil {
		return Unchanged, err
	}
	var cur string
	err = tx.QueryRowContext(ctx, `SELECT content_hash FROM worlds WHERE id = ?`, w.ID).Scan(&cur)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Unchanged, fmt.Errorf("read world %s: %w", w.ID, err)
	}
	if exists && cur == hash {
		return Unchanged, nil
	}
	hero, err := resolveAsset(ctx, tx, w.HeroAsset)
	if err != nil {
		return Unchanged, fmt.Errorf("world %s hero: %w", w.ID, err)
	}
	now := r.stamp()
	_, err = tx.ExecContext(ctx, `INSERT INTO worlds (id, ord, name_fr, name_en, tagline_fr, tagline_en, hook_fr, hook_en, accent_color, grade_min, grade_max, strand, hero_asset_id, status, content_hash, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET ord=excluded.ord, name_fr=excluded.name_fr, name_en=excluded.name_en, tagline_fr=excluded.tagline_fr,
		  tagline_en=excluded.tagline_en, hook_fr=excluded.hook_fr, hook_en=excluded.hook_en, accent_color=excluded.accent_color,
		  grade_min=excluded.grade_min, grade_max=excluded.grade_max, strand=excluded.strand, hero_asset_id=excluded.hero_asset_id,
		  status=excluded.status, content_hash=excluded.content_hash, updated_at=excluded.updated_at`,
		w.ID, w.Ord, w.Name.FR, w.Name.EN, w.Tagline.FR, w.Tagline.EN, w.Hook.FR, w.Hook.EN, w.AccentColor, w.GradeMin, w.GradeMax, w.Strand, hero, w.Status, hash, now)
	if err != nil {
		return Unchanged, fmt.Errorf("write world %s: %w", w.ID, err)
	}
	if err := r.catalogAudit(ctx, tx, actor, "world.update", "world", w.ID); err != nil {
		return Unchanged, err
	}
	if err := r.Out.Enqueue(ctx, tx, mongosync.KindContentWorld, w.ID, ""); err != nil {
		return Unchanged, err
	}
	if exists {
		return Changed, nil
	}
	return Added, nil
}

// UpsertQuest inserts or updates a quest when its content hash changed.
func (r *Repo) UpsertQuest(ctx context.Context, tx *sql.Tx, qu content.Quest, actor string) (Change, error) {
	if qu.Status == "" {
		qu.Status = "published"
	}
	hash, err := content.Hash(qu)
	if err != nil {
		return Unchanged, err
	}
	var cur string
	err = tx.QueryRowContext(ctx, `SELECT content_hash FROM world_quests WHERE id = ?`, qu.ID).Scan(&cur)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Unchanged, fmt.Errorf("read quest %s: %w", qu.ID, err)
	}
	if exists && cur == hash {
		return Unchanged, nil
	}
	icon, err := resolveAsset(ctx, tx, qu.IconAsset)
	if err != nil {
		return Unchanged, fmt.Errorf("quest %s icon: %w", qu.ID, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO world_quests (id, world_id, ord, skill_id, name_fr, name_en, goal_fr, goal_en, grade_min, grade_max, target_count, icon_asset_id, status, content_hash, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET world_id=excluded.world_id, ord=excluded.ord, skill_id=excluded.skill_id, name_fr=excluded.name_fr,
		  name_en=excluded.name_en, goal_fr=excluded.goal_fr, goal_en=excluded.goal_en, grade_min=excluded.grade_min, grade_max=excluded.grade_max,
		  target_count=excluded.target_count, icon_asset_id=excluded.icon_asset_id, status=excluded.status, content_hash=excluded.content_hash,
		  updated_at=excluded.updated_at`,
		qu.ID, qu.World, qu.Ord, qu.Skill, qu.Name.FR, qu.Name.EN, qu.Goal.FR, qu.Goal.EN, qu.GradeMin, qu.GradeMax, qu.TargetCount, icon, qu.Status, hash, r.stamp())
	if err != nil {
		return Unchanged, fmt.Errorf("write quest %s: %w", qu.ID, err)
	}
	if err := r.catalogAudit(ctx, tx, actor, "quest.update", "quest", qu.ID); err != nil {
		return Unchanged, err
	}
	if err := r.Out.Enqueue(ctx, tx, mongosync.KindContentQuest, qu.ID, ""); err != nil {
		return Unchanged, err
	}
	if exists {
		return Changed, nil
	}
	return Added, nil
}

// catalogAudit writes the general admin audit row of a world or quest change.
func (r *Repo) catalogAudit(ctx context.Context, tx *sql.Tx, actor, action, targetType, id string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO admin_audit (id, at, actor, action, target_type, target_id, detail) VALUES (?,?,?,?,?,?,'')`,
		uuid.NewString(), r.stamp(), actor, action, targetType, id)
	if err != nil {
		return fmt.Errorf("write audit: %w", err)
	}
	return nil
}

// Worlds lists every non-retired world in display order.
func (r *Repo) Worlds(ctx context.Context, q Querier) ([]WorldRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, ord, name_fr, name_en, tagline_fr, tagline_en, hook_fr, hook_en, accent_color, grade_min, grade_max, strand,
		COALESCE(hero_asset_id,''), status, content_hash FROM worlds WHERE status <> 'retired' ORDER BY ord`)
	if err != nil {
		return nil, fmt.Errorf("list worlds: %w", err)
	}
	defer rows.Close()
	var out []WorldRow
	for rows.Next() {
		var w WorldRow
		if err := rows.Scan(&w.ID, &w.Ord, &w.Name.FR, &w.Name.EN, &w.Tagline.FR, &w.Tagline.EN, &w.Hook.FR, &w.Hook.EN, &w.AccentColor,
			&w.GradeMin, &w.GradeMax, &w.Strand, &w.HeroAssetID, &w.Status, &w.Hash); err != nil {
			return nil, fmt.Errorf("scan world: %w", err)
		}
		w.HeroAsset = w.HeroAssetID
		out = append(out, w)
	}
	return out, rows.Err()
}

// Quests lists quests in display order; worldID "" lists all of them.
func (r *Repo) Quests(ctx context.Context, q Querier, worldID string) ([]QuestRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, world_id, ord, skill_id, name_fr, name_en, goal_fr, goal_en, grade_min, grade_max, target_count,
		COALESCE(icon_asset_id,''), status, content_hash FROM world_quests WHERE status <> 'retired' AND (? = '' OR world_id = ?) ORDER BY world_id, ord`, worldID, worldID)
	if err != nil {
		return nil, fmt.Errorf("list quests: %w", err)
	}
	defer rows.Close()
	var out []QuestRow
	for rows.Next() {
		var x QuestRow
		if err := rows.Scan(&x.ID, &x.World, &x.Ord, &x.Skill, &x.Name.FR, &x.Name.EN, &x.Goal.FR, &x.Goal.EN, &x.GradeMin, &x.GradeMax,
			&x.TargetCount, &x.IconAssetID, &x.Status, &x.Hash); err != nil {
			return nil, fmt.Errorf("scan quest: %w", err)
		}
		x.IconAsset = x.IconAssetID
		out = append(out, x)
	}
	return out, rows.Err()
}

// BankCount is the number of published questions of one quest.
type BankCount struct {
	World, Skill string
	Published    int
}

// BankCounts feeds the skill graph: how many published questions each (world, skill) has, and every quest
// (even with 0 questions) so the world lists its skills.
func (r *Repo) BankCounts(ctx context.Context, q Querier) ([]BankCount, error) {
	rows, err := q.QueryContext(ctx, `SELECT wq.world_id, wq.skill_id,
		(SELECT COUNT(*) FROM bank_questions b WHERE b.world_id = wq.world_id AND b.skill_id = wq.skill_id AND b.status = 'published')
		FROM world_quests wq WHERE wq.status = 'published'`)
	if err != nil {
		return nil, fmt.Errorf("count bank: %w", err)
	}
	defer rows.Close()
	var out []BankCount
	for rows.Next() {
		var c BankCount
		if err := rows.Scan(&c.World, &c.Skill, &c.Published); err != nil {
			return nil, fmt.Errorf("scan bank count: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Misconception is one known error code.
type Misconception struct {
	Code, Skill, DescFR, DescEN, Source string
}

// UpsertMisconceptions stores the codes (curriculum codes and mis_* codes) and returns how many rows changed.
func (r *Repo) UpsertMisconceptions(ctx context.Context, tx *sql.Tx, list []Misconception) (int, error) {
	n := 0
	for _, m := range list {
		res, err := tx.ExecContext(ctx, `INSERT INTO misconceptions (code, skill_id, desc_fr, desc_en, source) VALUES (?,?,?,?,?)
			ON CONFLICT(code) DO UPDATE SET skill_id=excluded.skill_id, desc_fr=excluded.desc_fr, desc_en=excluded.desc_en, source=excluded.source
			WHERE misconceptions.skill_id IS NOT excluded.skill_id OR misconceptions.desc_fr <> excluded.desc_fr
			   OR misconceptions.desc_en <> excluded.desc_en OR misconceptions.source <> excluded.source`,
			m.Code, nullable(m.Skill), m.DescFR, m.DescEN, m.Source)
		if err != nil {
			return n, fmt.Errorf("write misconception %s: %w", m.Code, err)
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n++
		}
	}
	return n, nil
}

// MisconceptionCodes returns the set of known codes.
func (r *Repo) MisconceptionCodes(ctx context.Context, q Querier) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT code FROM misconceptions`)
	if err != nil {
		return nil, fmt.Errorf("list misconceptions: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out[c] = true
	}
	return out, rows.Err()
}

// SeedRun is one row of seed_runs.
type SeedRun struct {
	ID, Actor, GitRev, FilesHash, BackupPath, Status string
	DryRun                                           bool
	Added, Changed, Retired, Unchanged               int
}

// StartRun records a seed run as started.
func (r *Repo) StartRun(ctx context.Context, tx *sql.Tx, run SeedRun) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO seed_runs (id, at, actor, dry_run, git_rev, files_hash, backup_path, status) VALUES (?,?,?,?,?,?,?,'started')`,
		run.ID, r.stamp(), run.Actor, boolInt(run.DryRun), nullable(run.GitRev), nullable(run.FilesHash), nullable(run.BackupPath))
	if err != nil {
		return fmt.Errorf("start seed run: %w", err)
	}
	return nil
}

// FinishRun stores the counts and the final status (ok | failed).
func (r *Repo) FinishRun(ctx context.Context, tx *sql.Tx, run SeedRun) error {
	_, err := tx.ExecContext(ctx, `UPDATE seed_runs SET added=?, changed=?, retired=?, unchanged=?, status=? WHERE id=?`,
		run.Added, run.Changed, run.Retired, run.Unchanged, run.Status, run.ID)
	if err != nil {
		return fmt.Errorf("finish seed run: %w", err)
	}
	return nil
}

// SeedSourced maps the id of every question loaded from a seed file to its content hash (retire candidates).
func (r *Repo) SeedSourced(ctx context.Context, q Querier) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, content_hash FROM bank_questions WHERE source LIKE 'seed:%' AND status <> 'retired'`)
	if err != nil {
		return nil, fmt.Errorf("list seeded questions: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, h string
		if err := rows.Scan(&id, &h); err != nil {
			return nil, err
		}
		out[id] = h
	}
	return out, rows.Err()
}
