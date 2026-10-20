// Package appconfig is the runtime configuration the admin edits: versioned, validated, audited, cached.
//
// Go for Spring people: think of a tiny "feature flag + settings service" backed by one SQL table. Each namespace
// ("music", "voice"...) is one JSON document with a typed struct, a default and a validator. A save never updates
// a row: it inserts version N+1 (so old, new, who and when are all kept) and a rollback is just another insert.
// The Store keeps the current documents in memory (Spring: a @Cacheable bean) and drops them after every write.
package appconfig

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// Errors the admin API maps to HTTP statuses.
var (
	ErrUnknown         = errors.New("unknown config namespace")
	ErrConflict        = errors.New("config changed since you loaded it")
	ErrNothingToRevert = errors.New("no earlier version to roll back to")
)

// ValidationError is a rule violation in the submitted document (HTTP 422).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// Doc is one namespace as stored: version 0 means "never saved, these are the defaults".
type Doc struct {
	Name      string          `json:"name"`
	Version   int             `json:"version"`
	UpdatedAt string          `json:"updated_at"`
	UpdatedBy string          `json:"updated_by"`
	Value     json.RawMessage `json:"value"`
}

// Version is one history row.
type Version struct {
	Version  int             `json:"version"`
	Action   string          `json:"action"`
	Actor    string          `json:"actor"`
	At       string          `json:"at"`
	OldValue json.RawMessage `json:"old_value"` // null when nothing was saved before
	Value    json.RawMessage `json:"value"`
}

// spec is one registered namespace. fresh returns a pointer to the defaults; check validates the typed value.
type spec struct {
	fresh func() any
	check func(any) error
}

const tsLayout = "2006-01-02T15:04:05.000Z" // same text format as SQLite strftime and admin_audit

// cacheTTL bounds staleness if another process ever writes the table (one process today: writes invalidate at once).
const cacheTTL = 30 * time.Second

// Store reads and writes the documents.
type Store struct {
	db    *store.Store
	now   func() time.Time
	specs map[string]spec
	order []string

	mu     sync.RWMutex
	cache  map[string]cached
	gen    int // bumped by every write; the public document cache is valid for one generation
	public *publicCache
}

type cached struct {
	doc Doc
	at  time.Time
}

// New builds the store. welcomeGift seeds the default of platform.welcome_gift (env FEATURE_WELCOME_GIFT).
func New(db *store.Store, welcomeGift bool) *Store {
	s := &Store{db: db, now: time.Now, specs: map[string]spec{}, cache: map[string]cached{}}
	add := func(name string, fresh func() any, check func(any) error) {
		s.specs[name] = spec{fresh, check}
		s.order = append(s.order, name)
	}
	add(NSPlatform, func() any { p := platformDefault(welcomeGift); return &p }, func(v any) error { return v.(*Platform).check() })
	add(NSMusic, func() any { m := musicDefault(); return &m }, func(v any) error { return v.(*Music).check() })
	add(NSVoice, func() any { m := voiceDefault(); return &m }, func(v any) error { return v.(*Voice).check() })
	add(NSScenes, func() any { m := scenesDefault(); return &m }, func(v any) error { return v.(*Scenes).check() })
	add(NSFeed, func() any { m := feedDefault(); return &m }, func(v any) error { return v.(*Feed).check() })
	add(NSMaintenance, func() any { m := maintenanceDefault(); return &m }, func(v any) error { return v.(*Maintenance).check() })
	add(NSAI, func() any { m := aiDefault(); return &m }, func(any) error { return nil })
	add(NSCopy, func() any { m := copyDefault(); return &m }, func(v any) error { return v.(*Copy).check() })
	return s
}

// Names lists the namespaces in display order.
func (s *Store) Names() []string { return append([]string(nil), s.order...) }

// Defaults returns the built-in document of a namespace.
func (s *Store) Defaults(ns string) (json.RawMessage, error) {
	sp, ok := s.specs[ns]
	if !ok {
		return nil, ErrUnknown
	}
	return json.Marshal(sp.fresh())
}

// Current returns the saved document, or version 0 with the defaults. Reads are served from memory.
func (s *Store) Current(ctx context.Context, ns string) (Doc, error) {
	if _, ok := s.specs[ns]; !ok {
		return Doc{}, ErrUnknown
	}
	s.mu.RLock()
	c, ok := s.cache[ns]
	gen := s.gen // a write between the SELECT and the cache fill must win: remember the generation first
	s.mu.RUnlock()
	if ok && s.now().Sub(c.at) < cacheTTL {
		return c.doc, nil
	}
	doc := Doc{Name: ns}
	var raw string
	err := s.db.R.QueryRowContext(ctx, `SELECT version, value, at, actor FROM app_config WHERE ns = ? ORDER BY version DESC LIMIT 1`, ns).
		Scan(&doc.Version, &raw, &doc.UpdatedAt, &doc.UpdatedBy)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if doc.Value, err = s.Defaults(ns); err != nil {
			return Doc{}, err
		}
	case err != nil:
		return Doc{}, fmt.Errorf("read config %s: %w", ns, err)
	default:
		doc.Value = json.RawMessage(raw)
	}
	s.mu.Lock()
	if s.gen == gen {
		s.cache[ns] = cached{doc, s.now()}
	}
	s.mu.Unlock()
	return doc, nil
}

// Get decodes a namespace into its typed struct. Defaults fill any key the stored JSON lacks, and a read problem
// returns the defaults (a config glitch must never fail a child's request).
func Get[T any](ctx context.Context, s *Store, ns string) T {
	v := s.specs[ns].fresh().(*T)
	if doc, err := s.Current(ctx, ns); err == nil {
		_ = json.Unmarshal(doc.Value, v) // lenient on read: unknown or missing keys are ignored
	}
	return *v
}

// normalize decodes raw over the defaults (omitted keys keep their default), rejects unknown keys, runs the
// validator and returns the canonical JSON that gets stored.
func (s *Store) normalize(ns string, raw []byte) (json.RawMessage, error) {
	sp, ok := s.specs[ns]
	if !ok {
		return nil, ErrUnknown
	}
	v := sp.fresh()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return nil, &ValidationError{"invalid config document: " + err.Error()}
	}
	if err := sp.check(v); err != nil {
		return nil, &ValidationError{err.Error()}
	}
	return json.Marshal(v)
}

// Set validates raw and saves it as the next version. expected (optional) is the version the editor loaded: a
// different current version answers ErrConflict so two admins never overwrite each other silently.
func (s *Store) Set(ctx context.Context, ns string, raw []byte, actor string, expected *int) (Doc, error) {
	val, err := s.normalize(ns, raw)
	if err != nil {
		return Doc{}, err
	}
	return s.write(ctx, ns, actor, "set", expected, func(*sql.Tx, int, string) (json.RawMessage, error) { return val, nil })
}

// Rollback saves the value that was in force before the current change (or the value of version `to`) as a NEW
// version. Undoing the very first save restores the defaults.
func (s *Store) Rollback(ctx context.Context, ns, actor string, to *int) (Doc, error) {
	return s.write(ctx, ns, actor, "rollback", nil, func(tx *sql.Tx, curVer int, _ string) (json.RawMessage, error) {
		if curVer == 0 {
			return nil, ErrNothingToRevert
		}
		q, arg := `SELECT old_value FROM app_config WHERE ns = ? AND version = ?`, curVer
		if to != nil {
			q, arg = `SELECT value FROM app_config WHERE ns = ? AND version = ?`, *to
		}
		var prev string
		if err := tx.QueryRowContext(ctx, q, ns, arg).Scan(&prev); errors.Is(err, sql.ErrNoRows) {
			return nil, &ValidationError{fmt.Sprintf("version %d does not exist", *to)}
		} else if err != nil {
			return nil, err
		}
		if prev == "" {
			return s.Defaults(ns)
		}
		return s.normalize(ns, []byte(prev)) // an old document must still pass today's rules
	})
}

// write is the shared transaction: read current, build the new value, insert version+1, add the admin_audit row.
func (s *Store) write(ctx context.Context, ns, actor, action string, expected *int,
	build func(tx *sql.Tx, curVer int, curVal string) (json.RawMessage, error)) (Doc, error) {
	if _, ok := s.specs[ns]; !ok {
		return Doc{}, ErrUnknown
	}
	at := s.now().UTC().Format(tsLayout)
	var out Doc
	err := s.db.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var curVer int
		var curVal sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT version, value FROM app_config WHERE ns = ? ORDER BY version DESC LIMIT 1`, ns).Scan(&curVer, &curVal)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if expected != nil && *expected != curVer {
			return ErrConflict
		}
		val, err := build(tx, curVer, curVal.String)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO app_config (ns, version, value, old_value, action, actor, at) VALUES (?,?,?,?,?,?,?)`,
			ns, curVer+1, string(val), curVal.String, action, actor, at); err != nil {
			return err
		}
		detail := fmt.Sprintf("%s v%d -> v%d", ns, curVer, curVer+1)
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_audit (id, at, actor, action, target_type, target_id, detail) VALUES (?,?,?,?,?,?,?)`,
			uuid.NewString(), at, actor, "appconfig."+action, "appconfig", ns, detail); err != nil {
			return err
		}
		out = Doc{Name: ns, Version: curVer + 1, UpdatedAt: at, UpdatedBy: actor, Value: val}
		return nil
	})
	if err != nil {
		return Doc{}, err
	}
	s.invalidate()
	return out, nil
}

// invalidate drops every cached document and the public document (called after each commit).
func (s *Store) invalidate() {
	s.mu.Lock()
	s.gen++
	s.cache = map[string]cached{}
	s.public = nil
	s.mu.Unlock()
}

// Versions lists the history of a namespace, newest first.
func (s *Store) Versions(ctx context.Context, ns string, limit int) ([]Version, error) {
	if _, ok := s.specs[ns]; !ok {
		return nil, ErrUnknown
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT version, action, actor, at, old_value, value FROM app_config WHERE ns = ? ORDER BY version DESC LIMIT ?`, ns, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		var old, val string
		if err := rows.Scan(&v.Version, &v.Action, &v.Actor, &v.At, &old, &val); err != nil {
			return nil, err
		}
		v.OldValue, v.Value = json.RawMessage("null"), json.RawMessage(val)
		if old != "" {
			v.OldValue = json.RawMessage(old)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
