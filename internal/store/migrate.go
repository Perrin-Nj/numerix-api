package store

import (
	"context"
	"database/sql"
	"embed" // go:embed copies files into the binary at build time (Spring: classpath resources)
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// migrationFiles holds every migrations/*.sql file inside the executable.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies, in file-name order, every migration not yet recorded in schema_migrations
// (Spring: Flyway, in 30 lines). Running it twice is safe: applied versions are skipped.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.W.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names) // 0001_..., 0002_... order

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var done int
		if err := s.W.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&done); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if done > 0 {
			continue // already applied
		}
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		// Each migration and its bookkeeping row commit together or not at all.
		err = s.WithWriteTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version)
			return err
		})
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
	}
	return nil
}
