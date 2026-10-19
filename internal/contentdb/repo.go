package contentdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// tsLayout is the timestamp text stored everywhere in the database (sorts as text).
const tsLayout = "2006-01-02T15:04:05.000Z"

// Errors the callers map to HTTP statuses or CLI messages. Compare with errors.Is.
var (
	ErrNotFound        = errors.New("contentdb: not found")
	ErrUnknownQuest    = errors.New("contentdb: unknown quest")
	ErrQuestMismatch   = errors.New("contentdb: question skill differs from its quest")
	ErrVersionConflict = errors.New("contentdb: version conflict")
	ErrBadTransition   = errors.New("contentdb: transition not allowed")
	ErrNeedsReviewer   = errors.New("contentdb: an AI draft needs a reviewer other than its author")
)

// InvalidError carries the blocking issues when a publish is refused.
type InvalidError struct{ Issues []content.Issue }

func (e *InvalidError) Error() string {
	return fmt.Sprintf("contentdb: question is invalid (%d issues)", len(content.Errors(e.Issues)))
}

// Repo is the question-bank repository. Create it with New.
type Repo struct {
	DB    *store.Store
	Out   *mongosync.Outbox // nil or disabled = no Mongo events
	Now   func() time.Time  // replaced in tests
	Cache *Cache
}

// New builds a Repo with a 2048-entry cache.
func New(db *store.Store, out *mongosync.Outbox) *Repo {
	return &Repo{DB: db, Out: out, Now: func() time.Time { return time.Now().UTC() }, Cache: NewCache(2048)}
}

// stamp formats now for storage.
func (r *Repo) stamp() string { return r.Now().UTC().Format(tsLayout) }

// Querier is satisfied by *sql.DB and *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Row is one stored bank question with its bookkeeping columns.
type Row struct {
	Q                       content.Question
	WorldID                 string
	Hash                    string
	PublishedAt, ReviewedAt string
	CreatedAt, UpdatedAt    string
}

// SaveInput describes one save of a question.
type SaveInput struct {
	Q             content.Question
	Actor         string // admin name or "seed:<run id>"
	Action        string // create | edit | submit | publish | unpublish | retire | seed | rollback | restore
	ExpectVersion int    // 0 = no check; otherwise the version the editor saw (optimistic lock)
	KeepStatus    bool   // seed mode: an unchanged hash leaves the row alone even if the status differs
	Detail        string // short note for the audit trail
}

// SaveResult says what happened.
type SaveResult struct {
	Created, Changed bool
	Version          int
	Hash             string
}

// questMeta reads the world and skill of a quest.
func questMeta(ctx context.Context, q Querier, id string) (world, skill string, err error) {
	err = q.QueryRowContext(ctx, `SELECT world_id, skill_id FROM world_quests WHERE id = ?`, id).Scan(&world, &skill)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("%w: %q", ErrUnknownQuest, id)
	}
	return world, skill, err
}

// Save inserts or updates a question inside the caller's transaction.
func (r *Repo) Save(ctx context.Context, tx *sql.Tx, in SaveInput) (SaveResult, error) {
	q := in.Q
	world, skill, err := questMeta(ctx, tx, q.Quest)
	if err != nil {
		return SaveResult{}, err
	}
	if q.Skill != skill {
		return SaveResult{}, fmt.Errorf("%w: question %s has %s, quest %s has %s", ErrQuestMismatch, q.ID, q.Skill, q.Quest, skill)
	}
	hash, err := q.ContentHash()
	if err != nil {
		return SaveResult{}, err
	}
	var curVersion int
	var curHash, curStatus string
	err = tx.QueryRowContext(ctx, `SELECT version, content_hash, status FROM bank_questions WHERE id = ?`, q.ID).Scan(&curVersion, &curHash, &curStatus)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SaveResult{}, fmt.Errorf("read question %s: %w", q.ID, err)
	}
	if exists {
		if in.ExpectVersion != 0 && in.ExpectVersion != curVersion {
			return SaveResult{}, fmt.Errorf("%w: question %s is at version %d, editor had %d", ErrVersionConflict, q.ID, curVersion, in.ExpectVersion)
		}
		if curHash == hash && (in.KeepStatus || curStatus == q.Status) {
			return SaveResult{Version: curVersion, Hash: hash}, nil // nothing to write
		}
	}
	version := curVersion + 1 // 1 for a new question
	q.Version = version
	body, err := content.Canonical(q)
	if err != nil {
		return SaveResult{}, err
	}
	now := r.stamp()
	var publishedAt, retiredAt any
	if q.Status == content.StatusPublished {
		publishedAt = now
	}
	if q.Status == content.StatusRetired {
		retiredAt = now
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bank_questions (id, world_id, quest_id, skill_id, type, answer_kind, grade, difficulty, status, version,
		content_json, content_hash, author, source, ai_drafted, reviewed_by, published_at, retired_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET world_id=excluded.world_id, quest_id=excluded.quest_id, skill_id=excluded.skill_id, type=excluded.type,
		  answer_kind=excluded.answer_kind, grade=excluded.grade, difficulty=excluded.difficulty, status=excluded.status, version=excluded.version,
		  content_json=excluded.content_json, content_hash=excluded.content_hash, author=excluded.author, source=excluded.source,
		  ai_drafted=excluded.ai_drafted, reviewed_by=excluded.reviewed_by,
		  published_at=CASE WHEN excluded.status='published' THEN excluded.published_at ELSE bank_questions.published_at END,
		  retired_at=CASE WHEN excluded.status='retired' THEN excluded.retired_at ELSE NULL END,
		  updated_at=excluded.updated_at`,
		q.ID, world, q.Quest, q.Skill, q.Type, q.Answer.Kind, q.Grade, q.Difficulty, q.Status, version,
		string(body), hash, q.Author, q.Source, boolInt(q.AIDrafted), nullable(q.ReviewedBy), publishedAt, retiredAt, now, now)
	if err != nil {
		return SaveResult{}, fmt.Errorf("write question %s: %w", q.ID, err)
	}
	if err := r.replaceChildren(ctx, tx, q); err != nil {
		return SaveResult{}, err
	}
	if err := r.record(ctx, tx, q.ID, version, curVersion, q.Status, string(body), hash, in.Actor, in.Action, in.Detail); err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Created: !exists, Changed: true, Version: version, Hash: hash}, nil
}

// replaceChildren rewrites the derived rows (tags, choices, media) of a question.
func (r *Repo) replaceChildren(ctx context.Context, tx *sql.Tx, q content.Question) error {
	for _, table := range []string{"question_tags", "question_choices", "question_media"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE question_id = ?`, q.ID); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	for _, t := range q.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO question_tags (question_id, tag) VALUES (?, ?)`, q.ID, strings.ToLower(strings.TrimSpace(t))); err != nil {
			return fmt.Errorf("write tag: %w", err)
		}
	}
	for i, c := range q.Choices {
		_, err := tx.ExecContext(ctx, `INSERT INTO question_choices (question_id, choice_id, ord, text_fr, text_en, is_correct, misconception, is_random)
			VALUES (?,?,?,?,?,?,?,?)`, q.ID, c.ID, i, c.Text.FR, c.Text.EN, boolInt(q.Answer.Kind == content.KindChoice && c.ID == q.Answer.Correct), nullable(c.Misconception), boolInt(c.Random))
		if err != nil {
			return fmt.Errorf("write choice: %w", err)
		}
	}
	for i, m := range q.Media {
		var crit any
		if m.Criteria != nil {
			b, err := json.Marshal(m.Criteria)
			if err != nil {
				return err
			}
			crit = string(b)
		}
		var assetID any
		if m.Asset != "" {
			var id string
			switch err := tx.QueryRowContext(ctx, `SELECT id FROM assets WHERE id = ? OR alias = ?`, m.Asset, m.Asset).Scan(&id); {
			case err == nil:
				assetID = id
			case !errors.Is(err, sql.ErrNoRows):
				return fmt.Errorf("resolve asset %q: %w", m.Asset, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO question_media (question_id, role, ord, asset_ref, asset_id, criteria_json) VALUES (?,?,?,?,?,?)`,
			q.ID, m.Role, i, nullable(m.Asset), assetID, crit); err != nil {
			return fmt.Errorf("write media: %w", err)
		}
	}
	return nil
}

// record writes the version snapshot, the two audit rows and the Mongo events of one save.
func (r *Repo) record(ctx context.Context, tx *sql.Tx, id string, version, fromVersion int, status, body, hash, actor, action, detail string) error {
	now := r.stamp()
	if _, err := tx.ExecContext(ctx, `INSERT INTO question_versions (question_id, version, status, content_json, content_hash, actor, action, at) VALUES (?,?,?,?,?,?,?,?)`,
		id, version, status, body, hash, actor, action, now); err != nil {
		return fmt.Errorf("write version: %w", err)
	}
	if len(detail) > 300 {
		detail = detail[:300]
	}
	auditID := uuid.NewString()
	var from any
	if fromVersion > 0 {
		from = fromVersion
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO question_audit (id, at, actor, action, question_id, from_version, to_version, content_hash, detail) VALUES (?,?,?,?,?,?,?,?,?)`,
		auditID, now, actor, action, id, from, version, hash, detail); err != nil {
		return fmt.Errorf("write question audit: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_audit (id, at, actor, action, target_type, target_id, detail) VALUES (?,?,?,?,?,?,?)`,
		uuid.NewString(), now, actor, "question."+action, "question", id, detail); err != nil {
		return fmt.Errorf("write admin audit: %w", err)
	}
	if err := r.Out.Enqueue(ctx, tx, mongosync.KindQuestionBank, id, ""); err != nil {
		return err
	}
	return r.Out.Enqueue(ctx, tx, mongosync.KindQuestionAudit, auditID, "")
}

// boolInt is SQLite's way of storing a bool.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullable turns an empty string into SQL NULL.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Get reads one question by id.
func (r *Repo) Get(ctx context.Context, q Querier, id string) (Row, error) {
	return scanRow(q.QueryRowContext(ctx, rowSelect+` WHERE id = ?`, id))
}

const rowSelect = `SELECT content_json, world_id, content_hash, status, version, author, source, ai_drafted, COALESCE(reviewed_by,''),
	COALESCE(published_at,''), COALESCE(reviewed_at,''), created_at, updated_at FROM bank_questions`

// scanRow decodes one bank_questions row; the bookkeeping columns win over what the JSON says.
func scanRow(row interface{ Scan(...any) error }) (Row, error) {
	var out Row
	var body string
	var ai int
	err := row.Scan(&body, &out.WorldID, &out.Hash, &out.Q.Status, &out.Q.Version, &out.Q.Author, &out.Q.Source, &ai, &out.Q.ReviewedBy,
		&out.PublishedAt, &out.ReviewedAt, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Row{}, ErrNotFound
	}
	if err != nil {
		return Row{}, fmt.Errorf("scan question: %w", err)
	}
	status, version, author, source, reviewer := out.Q.Status, out.Q.Version, out.Q.Author, out.Q.Source, out.Q.ReviewedBy
	if err := json.Unmarshal([]byte(body), &out.Q); err != nil {
		return Row{}, fmt.Errorf("decode question: %w", err)
	}
	out.Q.Status, out.Q.Version, out.Q.Author, out.Q.Source, out.Q.ReviewedBy, out.Q.AIDrafted = status, version, author, source, reviewer, ai == 1
	return out, nil
}

// allowedMoves is the status machine: from -> allowed targets.
var allowedMoves = map[string][]string{
	content.StatusDraft:     {content.StatusReview, content.StatusPublished, content.StatusRetired},
	content.StatusReview:    {content.StatusDraft, content.StatusPublished, content.StatusRetired},
	content.StatusPublished: {content.StatusDraft, content.StatusReview, content.StatusRetired},
	content.StatusRetired:   {content.StatusDraft},
}

// actionFor names the audit action of a move.
func actionFor(from, to string) string {
	switch {
	case to == content.StatusPublished:
		return "publish"
	case from == content.StatusPublished:
		return "unpublish"
	case to == content.StatusReview:
		return "submit"
	case to == content.StatusRetired:
		return "retire"
	}
	return "edit"
}

// Transition moves a question to another status. validate (may be nil) runs when the target is review or published
// and any blocking issue refuses the move.
// An AI draft can only be published by a reviewer other than its author, who is then recorded as reviewed_by.
func (r *Repo) Transition(ctx context.Context, tx *sql.Tx, id, to, actor, comment string, validate func(*content.Question) []content.Issue) (SaveResult, error) {
	row, err := r.Get(ctx, tx, id)
	if err != nil {
		return SaveResult{}, err
	}
	from := row.Q.Status
	ok := false
	for _, t := range allowedMoves[from] {
		ok = ok || t == to
	}
	if !ok {
		return SaveResult{}, fmt.Errorf("%w: %s -> %s", ErrBadTransition, from, to)
	}
	q := row.Q
	q.Status = to
	if to == content.StatusPublished && q.AIDrafted && (actor == "" || actor == q.Author) {
		return SaveResult{}, ErrNeedsReviewer
	}
	if to == content.StatusPublished {
		q.ReviewedBy = actor // set before validating: the rule "an AI draft needs a reviewer" reads it
	}
	if (to == content.StatusPublished || to == content.StatusReview) && validate != nil { // only a valid question enters the review queue
		if issues := validate(&q); content.HasErrors(issues) {
			return SaveResult{}, &InvalidError{Issues: issues}
		}
	}
	return r.Save(ctx, tx, SaveInput{Q: q, Actor: actor, Action: actionFor(from, to), Detail: comment})
}

// VersionRow is one entry of a question's history.
type VersionRow struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
	Hash    string `json:"content_hash"`
	Actor   string `json:"actor"`
	Action  string `json:"action"`
	At      string `json:"at"`
}

// Versions lists the history of a question, newest first.
func (r *Repo) Versions(ctx context.Context, q Querier, id string) ([]VersionRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT version, status, content_hash, actor, action, at FROM question_versions WHERE question_id = ? ORDER BY version DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()
	out := []VersionRow{}
	for rows.Next() {
		var v VersionRow
		if err := rows.Scan(&v.Version, &v.Status, &v.Hash, &v.Actor, &v.Action, &v.At); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Snapshot returns the question exactly as it was at a version.
func (r *Repo) Snapshot(ctx context.Context, q Querier, id string, version int) (content.Question, error) {
	var body, status string
	err := q.QueryRowContext(ctx, `SELECT content_json, status FROM question_versions WHERE question_id = ? AND version = ?`, id, version).Scan(&body, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return content.Question{}, ErrNotFound
	}
	if err != nil {
		return content.Question{}, fmt.Errorf("read snapshot: %w", err)
	}
	var out content.Question
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return content.Question{}, fmt.Errorf("decode snapshot: %w", err)
	}
	out.Status = status
	return out, nil
}

// Restore saves an old version as a new draft version (history is never rewritten).
func (r *Repo) Restore(ctx context.Context, tx *sql.Tx, id string, version int, actor string) (SaveResult, error) {
	old, err := r.Snapshot(ctx, tx, id, version)
	if err != nil {
		return SaveResult{}, err
	}
	old.Status, old.ReviewedBy = content.StatusDraft, ""
	return r.Save(ctx, tx, SaveInput{Q: old, Actor: actor, Action: "restore", Detail: fmt.Sprintf("restored version %d", version)})
}

// RollbackRun undoes a seed run: questions it created are retired, questions it changed return to the
// snapshot that preceded the run. It returns how many questions it touched. Worlds and quests have no
// history: restore the pre-seed backup file for those (docs/DB_PROD_DEPLOY.md).
func (r *Repo) RollbackRun(ctx context.Context, tx *sql.Tx, runID, actor string) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT question_id, MIN(version), MAX(version) FROM question_versions WHERE actor = ? GROUP BY question_id ORDER BY question_id`, "seed:"+runID)
	if err != nil {
		return 0, fmt.Errorf("find run changes: %w", err)
	}
	type change struct {
		id       string
		min, max int
	}
	var list []change
	for rows.Next() {
		var c change
		if err := rows.Scan(&c.id, &c.min, &c.max); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan run change: %w", err)
		}
		list = append(list, c)
	}
	if err := rows.Close(); err != nil { // the cursor must be closed before the next statement on this transaction
		return 0, err
	}
	for _, c := range list {
		var q content.Question
		if c.min == 1 { // created by the run: retire it (rows are never deleted: served history keeps working)
			row, err := r.Get(ctx, tx, c.id)
			if err != nil {
				return 0, err
			}
			q = row.Q
			q.Status = content.StatusRetired
		} else { // changed by the run: go back to the version before the first change of the run
			if q, err = r.Snapshot(ctx, tx, c.id, c.min-1); err != nil {
				return 0, err
			}
		}
		if _, err := r.Save(ctx, tx, SaveInput{Q: q, Actor: actor, Action: "rollback", Detail: "rollback of seed run " + runID}); err != nil {
			return 0, err
		}
	}
	return len(list), nil
}

// AfterCommit must be called once the transaction that saved these ids has committed.
func (r *Repo) AfterCommit(ids ...string) { r.Cache.InvalidateID(ids...) }

// WithTx runs fn in a write transaction and invalidates the cache for ids afterwards.
func (r *Repo) WithTx(ctx context.Context, fn func(tx *sql.Tx) ([]string, error)) error {
	var ids []string
	err := r.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var e error
		ids, e = fn(tx)
		return e
	})
	if err == nil {
		r.AfterCommit(ids...)
	}
	return err
}
