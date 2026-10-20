package feedmedia

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// Repo reads the items from SQLite and keeps them in memory for a short time (Spring: a @Cacheable repository).
// 200 items are about 200 KB of JSON, so one read per minute is plenty and a page request never touches the table.
type Repo struct {
	s   *store.Store
	ttl time.Duration

	mu    sync.Mutex // protects the two fields below (Spring: synchronized)
	items []Item
	at    time.Time
}

// NewRepo builds a repository over the store; ttl is how long a loaded list stays fresh.
func NewRepo(s *store.Store, ttl time.Duration) *Repo { return &Repo{s: s, ttl: ttl} }

// All returns every item in the stable seed order. The slice is shared: callers must not modify it.
func (r *Repo) All(ctx context.Context) ([]Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock() // released when the method returns, whatever the path
	if r.items != nil && time.Since(r.at) < r.ttl {
		return r.items, nil
	}
	rows, err := r.s.R.QueryContext(ctx, `SELECT id, kind, world, topic, grade_min, body FROM feed_media ORDER BY sort`)
	if err != nil {
		return nil, fmt.Errorf("list feed media: %w", err)
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var body string
		if err := rows.Scan(&it.ID, &it.Kind, &it.World, &it.Topic, &it.GradeMin, &body); err != nil {
			return nil, fmt.Errorf("scan feed media: %w", err)
		}
		if err := json.Unmarshal([]byte(body), &it); err != nil { // the body repeats the columns; the columns win below
			return nil, fmt.Errorf("decode feed media %s: %w", it.ID, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	r.items, r.at = out, time.Now()
	return out, nil
}

// Forget drops the cached list (tests and feedseed call it after a write).
func (r *Repo) Forget() {
	r.mu.Lock()
	r.items = nil
	r.mu.Unlock()
}

// Result counts what Apply did.
type Result struct{ Inserted, Updated, Unchanged, Deleted int }

// Apply makes the table equal to items: new rows are inserted, changed rows (other checksum) updated, rows no longer
// in the list deleted. All or nothing: one transaction. Idempotent: a second run with the same items changes nothing.
func Apply(ctx context.Context, s *store.Store, items []Item, now time.Time) (Result, error) {
	var res Result
	err := s.WithWriteTx(ctx, func(tx *sql.Tx) error { // Spring: @Transactional
		have := map[string]string{} // id -> checksum already stored
		rows, err := tx.QueryContext(ctx, `SELECT id, checksum FROM feed_media`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, sum string
			if err := rows.Scan(&id, &sum); err != nil {
				_ = rows.Close()
				return err
			}
			have[id] = sum
		}
		if err := rows.Close(); err != nil {
			return err
		}
		seen := map[string]bool{}
		for i, it := range items {
			body, err := json.Marshal(it)
			if err != nil {
				return err
			}
			h := sha256.Sum256(append(body, byte(i), byte(i>>8))) // the position is part of the checksum
			sum := hex.EncodeToString(h[:])
			seen[it.ID] = true
			old, exists := have[it.ID]
			if exists && old == sum {
				res.Unchanged++
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO feed_media (id, kind, world, topic, grade_min, sort, body, checksum, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET kind = excluded.kind, world = excluded.world, topic = excluded.topic,
					grade_min = excluded.grade_min, sort = excluded.sort, body = excluded.body,
					checksum = excluded.checksum, updated_at = excluded.updated_at`,
				it.ID, it.Kind, it.World, it.Topic, it.GradeMin, i, string(body), sum, now.UTC().Format(time.RFC3339)); err != nil {
				return err
			}
			if exists {
				res.Updated++
			} else {
				res.Inserted++
			}
		}
		for id := range have {
			if !seen[id] {
				if _, err := tx.ExecContext(ctx, `DELETE FROM feed_media WHERE id = ?`, id); err != nil {
					return err
				}
				res.Deleted++
			}
		}
		return nil
	})
	return res, err
}

// ParseAll decodes items.json and validates every item and the whole set (unique ids). dir may be empty to skip file checks.
func ParseAll(raw []byte, dir string) ([]Item, []string) {
	var items []Item
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, []string{"items.json: " + err.Error()}
	}
	var problems []string
	ids := map[string]bool{}
	for _, it := range items {
		if ids[it.ID] {
			problems = append(problems, it.ID+": duplicate id")
		}
		ids[it.ID] = true
		problems = append(problems, it.Validate(dir)...)
	}
	return items, problems
}
