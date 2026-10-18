package mongosync

// Go for Spring people: tests live in the same package (package mongosync) so they can reach unexported
// helpers. A "fake" is a tiny hand-written stand-in for a dependency (Spring: a Mockito mock, but by hand).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// fakeSink records the last document per (kind,key) like Mongo would, and can be told to fail.
type fakeSink struct {
	mu    sync.Mutex
	docs  map[string]Op
	calls [][]Op // every Bulk call, to check batching
	err   error  // returned by Bulk when set
}

func newFakeSink() *fakeSink { return &fakeSink{docs: map[string]Op{}} }

func (f *fakeSink) Bulk(_ context.Context, ops []Op) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ops)
	if f.err != nil {
		return f.err
	}
	for _, op := range ops {
		if op.Delete {
			delete(f.docs, op.Kind+"|"+op.Key)
		} else {
			f.docs[op.Kind+"|"+op.Key] = op // same key = overwrite: this is the idempotency
		}
	}
	return nil
}
func (f *fakeSink) Upsert(ctx context.Context, op Op) error { return f.Bulk(ctx, []Op{op}) }
func (f *fakeSink) Close(context.Context) error             { return nil }

// echoResolver turns an event into an Op without touching SQLite.
type echoResolver struct{}

func (echoResolver) Resolve(_ context.Context, ev Event) (Op, error) {
	if ev.Kind == "skipme" {
		return Op{}, ErrSkip
	}
	return Op{Kind: ev.Kind, Key: "pseudo-" + ev.Key, Doc: map[string]any{"n": 1}}, nil
}

func openDB(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(context.Background(), t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newWorker(t *testing.T, db *store.Store, sink Sink) *Worker {
	t.Helper()
	w := NewWorker(db, sink, echoResolver{}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	w.rand = func(n int64) int64 { return n - 1 } // deterministic jitter: top of the range
	return w
}

func enqueue(t *testing.T, db *store.Store, o *Outbox, kind string, keys ...string) {
	t.Helper()
	err := db.WithWriteTx(context.Background(), func(tx *sql.Tx) error {
		for _, k := range keys {
			if err := o.Enqueue(context.Background(), tx, kind, k, ""); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *store.Store, where string) int {
	t.Helper()
	var n int
	if err := db.R.QueryRow(`SELECT COUNT(*) FROM outbox WHERE ` + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnqueueRollsBackWithTheCallerTransaction(t *testing.T) {
	db, o := openDB(t), NewOutbox(true)
	boom := errors.New("game change failed")
	err := db.WithWriteTx(context.Background(), func(tx *sql.Tx) error {
		if e := o.Enqueue(context.Background(), tx, KindBests, "c1", ""); e != nil {
			return e
		}
		return boom // the game change fails after the event was queued
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if n := count(t, db, "1=1"); n != 0 {
		t.Fatalf("rollback must leave no event, got %d", n)
	}
	enqueue(t, db, o, KindBests, "c1")
	if n := count(t, db, "sent_at IS NULL"); n != 1 {
		t.Fatalf("commit must keep the event, got %d", n)
	}
}

func TestDisabledModeWritesNothing(t *testing.T) {
	db := openDB(t)
	var nilOutbox *Outbox
	for _, o := range []*Outbox{nilOutbox, NewOutbox(false)} {
		enqueue(t, db, o, KindBests, "c1") // must not panic nor insert
	}
	if n := count(t, db, "1=1"); n != 0 {
		t.Fatalf("disabled outbox wrote %d rows", n)
	}
	d := Disabled{}
	if d.Bulk(context.Background(), []Op{{Kind: KindBests}}) != nil || d.Upsert(context.Background(), Op{}) != nil || d.Close(context.Background()) != nil {
		t.Fatal("disabled sink must accept everything")
	}
}

func TestBatchingAndCoalescing(t *testing.T) {
	db, o, sink := openDB(t), NewOutbox(true), newFakeSink()
	w := newWorker(t, db, sink)
	keys := make([]string, 250)
	for i := range keys {
		keys[i] = fmt.Sprintf("c%d", i)
	}
	enqueue(t, db, o, KindBests, keys...)
	enqueue(t, db, o, KindBests, "c0", "c0") // same key again: one write is enough
	time.Sleep(5 * time.Millisecond)
	w.Drain(context.Background())
	if len(sink.calls) != 3 { // 252 rows read as 100+100+52, de-duplicated inside each batch
		t.Fatalf("bulk calls = %d, want 3", len(sink.calls))
	}
	for _, c := range sink.calls {
		if len(c) > 100 {
			t.Fatalf("batch of %d exceeds 100", len(c))
		}
	}
	if len(sink.docs) != 250 {
		t.Fatalf("docs = %d, want 250", len(sink.docs))
	}
	if n := count(t, db, "sent_at IS NULL"); n != 0 {
		t.Fatalf("%d rows still pending", n)
	}
	if st := w.Stats(); st.Pending != 0 || st.LastSyncAt == nil || st.LastError != "" || st.Sent < 250 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestSkipAndReplayAreSafe(t *testing.T) {
	db, o, sink := openDB(t), NewOutbox(true), newFakeSink()
	w := newWorker(t, db, sink)
	enqueue(t, db, o, "skipme", "x")
	enqueue(t, db, o, KindFamily, "f1")
	time.Sleep(5 * time.Millisecond)
	w.Drain(context.Background())
	if len(sink.docs) != 1 || count(t, db, "sent_at IS NULL") != 0 {
		t.Fatalf("skip must be marked done without a write: %v", sink.docs)
	}
	// at-least-once: the same event delivered again leaves one identical document
	enqueue(t, db, o, KindFamily, "f1")
	time.Sleep(5 * time.Millisecond)
	w.Drain(context.Background())
	if len(sink.docs) != 1 {
		t.Fatalf("replay duplicated a document: %v", sink.docs)
	}
}

func TestRetryWithBackoffThenSuccess(t *testing.T) {
	db, o, sink := openDB(t), NewOutbox(true), newFakeSink()
	w := newWorker(t, db, sink)
	enqueue(t, db, o, KindBests, "c1")
	clock := time.Now().UTC().Add(time.Minute) // after the row's created_at, which uses the real DB clock
	w.now = func() time.Time { return clock }
	sink.err = errors.New("atlas down")
	if _, err := w.FlushOnce(context.Background()); err == nil {
		t.Fatal("want an error from the failing sink")
	}
	var attempts int
	if err := db.R.QueryRow(`SELECT attempts FROM outbox`).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("attempts = %d err=%v", attempts, err)
	}
	if st := w.Stats(); st.LastError == "" || st.Failures != 1 {
		t.Fatalf("stats after failure: %+v", st)
	}
	calls := len(sink.calls)
	sink.err = nil
	if n, _ := w.FlushOnce(context.Background()); n != 0 || len(sink.calls) != calls { // not due yet
		t.Fatalf("row retried before its backoff elapsed (n=%d)", n)
	}
	clock = clock.Add(time.Hour) // backoff over
	if n, err := w.FlushOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("retry: n=%d err=%v", n, err)
	}
	if len(sink.docs) != 1 || w.Stats().LastError != "" {
		t.Fatalf("not delivered after retry: %v %+v", sink.docs, w.Stats())
	}
}

func TestBackoffGrowsIsCappedAndJittered(t *testing.T) {
	w := newWorker(t, nil, newFakeSink())
	w.rand = func(n int64) int64 { return 0 } // bottom of the jitter range = d/2
	prev := time.Duration(0)
	for a := 0; a < 6; a++ {
		d := w.backoff(a)
		if d <= prev {
			t.Fatalf("attempt %d: %v not above %v", a, d, prev)
		}
		prev = d
	}
	if d := w.backoff(50); d > 15*time.Minute || d < 7*time.Minute {
		t.Fatalf("cap broken: %v", d)
	}
	w.rand = func(n int64) int64 { return n - 1 }
	if d := w.backoff(0); d > 5*time.Second+time.Millisecond || d < 2500*time.Millisecond {
		t.Fatalf("jitter range broken: %v", d)
	}
}

func TestRunStopsOnContextAndFlushesLast(t *testing.T) {
	db, o, sink := openDB(t), NewOutbox(true), newFakeSink()
	w := newWorker(t, db, sink)
	w.Interval = time.Hour // the ticker never fires: only the shutdown flush can deliver
	enqueue(t, db, o, KindBests, "c1")
	time.Sleep(5 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
	if len(sink.docs) != 1 {
		t.Fatalf("shutdown flush missed the event: %v", sink.docs)
	}
}

func TestPseudonymStableAndNotReversible(t *testing.T) {
	a, b := NewPseudonymizer(strings.Repeat("a", 32)), NewPseudonymizer(strings.Repeat("b", 32))
	id := "11111111-1111-4111-8111-111111111111"
	first, second := a.ID("child", id), a.ID("child", id) // two separate calls
	if first != second {
		t.Fatal("pseudonym must be stable")
	}
	if a.ID("child", id) == b.ID("child", id) {
		t.Fatal("another secret must give another pseudonym")
	}
	if a.ID("child", id) == a.ID("family", id) {
		t.Fatal("domains must be separated")
	}
	p := a.ID("child", id)
	if len(p) != 64 || strings.Contains(p, "1111") {
		t.Fatalf("pseudonym looks like the id or has the wrong size: %s", p)
	}
}
