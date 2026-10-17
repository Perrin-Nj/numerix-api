package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// open creates a fresh database file in a per-test temp directory.
func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrateIdempotentAndTables(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil { // second run must be a no-op
		t.Fatal(err)
	}
	for _, table := range []string{"parents", "refresh_tokens", "children", "child_settings", "consents", "subscriptions",
		"notifications", "push_subscriptions", "analytics_events", "questions", "child_questions", "sessions", "beats",
		"answers", "child_profiles", "feed_items", "reactions", "collection", "quests", "challenges", "gifts"} {
		var n int
		if err := s.R.QueryRowContext(ctx, `SELECT COUNT(1) FROM `+table).Scan(&n); err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
	var applied int
	_ = s.R.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations`).Scan(&applied)
	if applied != 1 {
		t.Fatalf("want 1 migration row, got %d", applied)
	}
}

func TestWithWriteTx(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	insert := func(tx *sql.Tx, email string) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO parents (id, email, password_hash) VALUES (?, ?, 'x')`, email, email)
		return err
	}
	// Rollback on error.
	boom := errors.New("boom")
	if err := s.WithWriteTx(ctx, func(tx *sql.Tx) error { _ = insert(tx, "a@x.test"); return boom }); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	// Commit on success.
	if err := s.WithWriteTx(ctx, func(tx *sql.Tx) error { return insert(tx, "b@x.test") }); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.R.QueryRowContext(ctx, `SELECT COUNT(1) FROM parents`).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 committed row, got %d", n)
	}
	// Unique email, case-insensitive.
	err := s.WithWriteTx(ctx, func(tx *sql.Tx) error { return insert(tx, "B@X.test") })
	if !IsUniqueViolation(err) {
		t.Fatalf("want unique violation, got %v", err)
	}
	// Foreign keys are enforced.
	err = s.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO children (id, parent_id, display_name) VALUES ('c','missing','Z')`)
		return e
	})
	if err == nil {
		t.Fatal("foreign key should be enforced")
	}
}
