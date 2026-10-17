// Package store owns the SQLite database: connections, migrations and the write-transaction helper.
//
// Go for Spring people:
//   - database/sql is the JDBC of Go. *sql.DB is a connection pool (Spring: DataSource), not one connection.
//   - modernc.org/sqlite is a pure Go SQLite driver (no C compiler needed). It registers itself under
//     the name "sqlite" when imported with a blank import (the _ means "import only for its side effect").
//   - SQLite allows ONE writer at a time. We therefore keep two pools: a write pool limited to a single
//     connection (writes queue up in Go, no "database is locked" errors) and a read pool for concurrent reads.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver with database/sql
)

// Store bundles the two pools (Spring: a repository layer root holding the DataSources).
type Store struct {
	W *sql.DB // write pool: exactly one connection
	R *sql.DB // read pool: many connections, query_only
}

// basePragmas are applied on every new connection through the DSN.
// WAL: readers do not block the writer. busy_timeout: wait 5 s instead of failing on a lock.
// foreign_keys: SQLite ignores FOREIGN KEY unless asked. synchronous NORMAL: safe with WAL and faster.
const basePragmas = "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"

// Open creates the directory if needed, opens both pools and runs the migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil { // make ./data when missing
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}
	// file: URI form with forward slashes works on Windows and Linux.
	uri := "file:" + filepath.ToSlash(path)

	// _txlock=immediate makes every BeginTx on this pool run "BEGIN IMMEDIATE": the write lock is taken
	// at the start, so a transaction never fails halfway because another writer got in first.
	w, err := sql.Open("sqlite", uri+"?"+basePragmas+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open write pool: %w", err)
	}
	w.SetMaxOpenConns(1) // single writer

	r, err := sql.Open("sqlite", uri+"?"+basePragmas+"&_pragma=query_only(1)")
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	r.SetMaxOpenConns(max(4, runtime.NumCPU())) // reads scale with CPUs
	r.SetConnMaxIdleTime(5 * time.Minute)

	s := &Store{W: w, R: r}
	if err := s.Ping(ctx); err != nil { // fail fast on a bad path or permissions
		_ = s.Close()
		return nil, err
	}
	if err := s.Migrate(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Close closes both pools.
func (s *Store) Close() error {
	return errors.Join(s.W.Close(), s.R.Close())
}

// Ping checks that both pools can reach the database (used by GET /ready).
func (s *Store) Ping(ctx context.Context) error {
	if err := s.W.PingContext(ctx); err != nil {
		return fmt.Errorf("ping write pool: %w", err)
	}
	if err := s.R.PingContext(ctx); err != nil {
		return fmt.Errorf("ping read pool: %w", err)
	}
	return nil
}

// WithWriteTx runs fn inside a write transaction (Spring: @Transactional on a method).
// It commits when fn returns nil and rolls back on error or panic. The transaction starts with
// BEGIN IMMEDIATE (see _txlock above), and the single write connection serialises callers.
func (s *Store) WithWriteTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin write tx: %w", err)
	}
	defer func() { // defer runs on every exit path, like finally
		if p := recover(); p != nil { // a panic inside fn: roll back, then keep panicking
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = errors.Join(err, rbErr)
			}
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit write tx: %w", err)
	}
	return nil
}

// IsUniqueViolation reports whether err is a SQLite UNIQUE/PRIMARY KEY conflict (Spring: DuplicateKeyException).
func IsUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
