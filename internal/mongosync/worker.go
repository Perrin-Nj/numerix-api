package mongosync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// tsLayout is the timestamp format used everywhere in the database (sorts as text).
const tsLayout = "2006-01-02T15:04:05.000Z"

// Event is one outbox row.
type Event struct {
	ID        int64
	Kind, Key string
	Payload   string
	CreatedAt time.Time
	Attempts  int
}

// Resolver turns an event into the current state to send. It reads SQLite (read pool), pseudonymises ids and
// applies the privacy rules. It returns ErrSkip when there is nothing to write.
type Resolver interface {
	Resolve(ctx context.Context, ev Event) (Op, error)
}

// Stats is what /admin/v1/health shows (public /health stays free of infrastructure details).
type Stats struct {
	Enabled    bool       `json:"enabled"`
	Pending    int        `json:"pending"`
	Sent       int64      `json:"sent_total"`
	Failures   int64      `json:"failures_total"`
	LastSyncAt *time.Time `json:"last_sync_at"`
	LastError  string     `json:"last_error"`
}

// Worker drains the outbox in batches (Spring: a @Scheduled bean). Build it with NewWorker.
type Worker struct {
	DB       *store.Store
	Sink     Sink
	Resolve  Resolver
	Log      *slog.Logger
	Batch    int           // rows per round trip (default 100)
	Interval time.Duration // tick period (default 5 s)

	now  func() time.Time    // clock, replaced in tests
	rand func(n int64) int64 // jitter source, replaced in tests

	mu sync.Mutex // guards st: Run writes it, /admin/v1/health reads it
	st Stats
}

// NewWorker applies the defaults. enabled=false only changes what Stats reports (the caller does not run it).
func NewWorker(db *store.Store, sink Sink, res Resolver, log *slog.Logger, enabled bool) *Worker {
	return &Worker{DB: db, Sink: sink, Resolve: res, Log: log, Batch: 100, Interval: 5 * time.Second,
		now: func() time.Time { return time.Now().UTC() }, rand: rand.Int64N, st: Stats{Enabled: enabled}}
}

// Stats returns a copy of the counters (safe to call from any goroutine).
func (w *Worker) Stats() Stats {
	if w == nil {
		return Stats{}
	}
	w.mu.Lock()
	defer w.mu.Unlock() // unlock on every exit path (Spring: try/finally)
	return w.st
}

// Backoff is the wait before attempt number attempts+1: 5 s doubling up to 15 min, then jittered into
// [d/2, d) so many failing rows do not retry in the same instant.
func (w *Worker) backoff(attempts int) time.Duration {
	d := min(5*time.Second<<min(attempts, 10), 15*time.Minute)
	return d/2 + time.Duration(w.rand(int64(d/2)+1))
}

// Run loops until ctx is cancelled, then tries one last flush so a clean shutdown loses nothing
// (anything unsent stays in the outbox for the next start anyway).
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			last, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second) // ctx is dead, use a fresh one
			defer cancel()
			w.Drain(last)
			return
		case <-t.C:
			w.Drain(ctx)
		}
	}
}

// Drain flushes batches until the backlog is empty, a batch fails, or ctx ends. It also cleans old rows.
func (w *Worker) Drain(ctx context.Context) {
	for ctx.Err() == nil {
		n, err := w.FlushOnce(ctx)
		if err != nil {
			w.Log.Warn("mongo sync batch failed", "err", err)
			break
		}
		if n < w.Batch { // short batch = nothing more is due
			break
		}
	}
	w.housekeeping(ctx)
}

// FlushOnce sends one batch of due rows. It returns how many rows it read. A non-nil error means the batch
// was recorded as failed (attempts+1, next_attempt_at pushed out) and will be retried.
func (w *Worker) FlushOnce(ctx context.Context) (int, error) {
	now := w.now()
	events, err := w.due(ctx, now)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	// Several rows for the same key collapse into one write: the resolver reads the CURRENT state anyway.
	type group struct {
		ev  Event
		ids []int64
	}
	groups := map[string]*group{}
	var order []string // keep first-seen order so batches stay deterministic
	for _, ev := range events {
		k := ev.Kind + "|" + ev.Key
		if g, ok := groups[k]; ok {
			g.ids = append(g.ids, ev.ID)
			continue
		}
		groups[k] = &group{ev: ev, ids: []int64{ev.ID}}
		order = append(order, k)
	}
	var ops []Op
	var okIDs, skipIDs []int64
	var firstErr error
	for _, k := range order {
		g := groups[k]
		op, rerr := w.Resolve.Resolve(ctx, g.ev)
		switch {
		case errors.Is(rerr, ErrSkip):
			skipIDs = append(skipIDs, g.ids...) // nothing to write: mark done
		case rerr != nil:
			firstErr = errors.Join(firstErr, fmt.Errorf("resolve %s: %w", g.ev.Kind, rerr))
			if e := w.fail(ctx, g.ids, now, rerr); e != nil {
				return len(events), e
			}
		default:
			ops = append(ops, op)
			okIDs = append(okIDs, g.ids...)
		}
	}
	if len(ops) > 0 {
		if err := w.Sink.Bulk(ctx, ops); err != nil {
			if e := w.fail(ctx, okIDs, now, err); e != nil {
				return len(events), e
			}
			return len(events), fmt.Errorf("bulk write: %w", err)
		}
	}
	if err := w.markSent(ctx, append(okIDs, skipIDs...), now); err != nil {
		return len(events), err // rows stay unsent: they will be sent again, which is safe (idempotent)
	}
	if len(okIDs) > 0 {
		w.mu.Lock()
		t := now
		w.st.LastSyncAt, w.st.Sent = &t, w.st.Sent+int64(len(ops))
		w.st.LastError = ""
		w.mu.Unlock()
	}
	return len(events), firstErr
}

// due loads the next rows that are ready to be sent, oldest first.
func (w *Worker) due(ctx context.Context, now time.Time) ([]Event, error) {
	rows, err := w.DB.R.QueryContext(ctx, `SELECT id, kind, key, payload, created_at, attempts FROM outbox
		WHERE sent_at IS NULL AND next_attempt_at <= ? ORDER BY id LIMIT ?`, now.Format(tsLayout), w.Batch)
	if err != nil {
		return nil, fmt.Errorf("read outbox: %w", err)
	}
	defer rows.Close() // always release the connection
	var out []Event
	for rows.Next() {
		var ev Event
		var at string
		if err := rows.Scan(&ev.ID, &ev.Kind, &ev.Key, &ev.Payload, &at, &ev.Attempts); err != nil {
			return nil, fmt.Errorf("scan outbox row: %w", err)
		}
		ev.CreatedAt, _ = time.Parse(tsLayout, at) // a bad stamp gives the zero time; the resolver copes
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ids builds "?,?,?" and the argument list for an IN clause.
func inClause(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")", args
}

// markSent flags rows as delivered.
func (w *Worker) markSent(ctx context.Context, ids []int64, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	in, args := inClause(ids)
	return w.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE outbox SET sent_at = ? WHERE id IN `+in, append([]any{now.Format(tsLayout)}, args...)...)
		if err != nil {
			return fmt.Errorf("mark sent: %w", err)
		}
		return nil
	})
}

// fail records a failed attempt for rows: attempts+1 and a later next_attempt_at (exponential, jittered).
func (w *Worker) fail(ctx context.Context, ids []int64, now time.Time, cause error) error {
	if len(ids) == 0 {
		return nil
	}
	msg := cause.Error()
	if len(msg) > 200 {
		msg = msg[:200] // keep the health output small
	}
	w.mu.Lock()
	w.st.Failures += int64(len(ids))
	w.st.LastError = msg
	w.mu.Unlock()
	in, args := inClause(ids)
	return w.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		// one UPDATE per row because the delay depends on that row's attempts
		rows, err := tx.QueryContext(ctx, `SELECT id, attempts FROM outbox WHERE id IN `+in, args...)
		if err != nil {
			return fmt.Errorf("read attempts: %w", err)
		}
		type row struct {
			id int64
			n  int
		}
		var list []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.n); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan attempts: %w", err)
			}
			list = append(list, r)
		}
		if err := rows.Close(); err != nil { // must close before running another statement on this tx
			return err
		}
		for _, r := range list {
			next := now.Add(w.backoff(r.n)).Format(tsLayout)
			if _, err := tx.ExecContext(ctx, `UPDATE outbox SET attempts = attempts + 1, next_attempt_at = ? WHERE id = ?`, next, r.id); err != nil {
				return fmt.Errorf("record failure: %w", err)
			}
		}
		return nil
	})
}

// housekeeping deletes rows sent more than a day ago and refreshes the pending counter.
func (w *Worker) housekeeping(ctx context.Context) {
	cutoff := w.now().Add(-24 * time.Hour).Format(tsLayout)
	if err := w.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE sent_at IS NOT NULL AND sent_at < ?`, cutoff)
		return err
	}); err != nil && ctx.Err() == nil {
		w.Log.Warn("outbox cleanup failed", "err", err)
	}
	var n int
	if err := w.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE sent_at IS NULL`).Scan(&n); err != nil {
		if ctx.Err() == nil {
			w.Log.Warn("outbox count failed", "err", err)
		}
		return
	}
	w.mu.Lock()
	w.st.Pending = n
	w.mu.Unlock()
}
