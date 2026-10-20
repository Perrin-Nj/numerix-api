package appconfig

// Go for Spring people: a table-driven test is a JUnit @ParameterizedTest. Each test opens a throw-away SQLite file
// (like @DataJpaTest with an embedded database).

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

func newStore(t *testing.T) (*Store, *store.Store) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db, false), db
}

func TestDefaultsWhenNothingSaved(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	for _, ns := range s.Names() {
		d, err := s.Current(ctx, ns)
		if err != nil || d.Version != 0 || len(d.Value) == 0 {
			t.Fatalf("%s: %v %+v", ns, err, d)
		}
		// every default must pass its own validator, or the first admin save of an untouched form would fail
		if _, err := s.normalize(ns, d.Value); err != nil {
			t.Fatalf("%s default is invalid: %v", ns, err)
		}
	}
	if m := Get[Music](ctx, s, NSMusic); !m.Enabled || m.DefaultVolume != 30 || len(m.Tracks) != 7 {
		t.Fatalf("music default: %+v", m)
	}
}

func TestSetValidation(t *testing.T) {
	s, _ := newStore(t)
	tests := []struct{ name, ns, body string }{
		{"volume too high", NSMusic, `{"default_volume":101}`},
		{"duplicate track", NSMusic, `{"tracks":[{"id":"a","title":{"fr":"A","en":"A"},"enabled":true},{"id":"a","title":{"fr":"A","en":"A"},"enabled":true}]}`},
		{"unknown key", NSMusic, `{"volumee":10}`},
		{"not an object", NSMusic, `[1]`},
		{"bad clip url", NSVoice, `{"clips":[{"line_id":"l1","locale":"fr-CA","url":"javascript:alert(1)","enabled":true}]}`},
		{"protocol relative url", NSVoice, `{"clips":[{"line_id":"l1","locale":"fr-CA","url":"//evil.example/a.mp3","enabled":true}]}`},
		{"bad clip locale", NSVoice, `{"clips":[{"line_id":"l1","locale":"de-DE","url":"/voice/a.mp3","enabled":true}]}`},
		{"unknown scene family", NSScenes, `{"families":{"cooking":{"enabled":true,"disabled_variants":[]}}}`},
		{"bad level", NSMaintenance, `{"level":"panic"}`},
		{"banner needs both languages", NSMaintenance, `{"enabled":true,"level":"info","message":{"fr":"Bientôt","en":""}}`},
		{"stretch out of range", NSFeed, `{"max_grade_stretch":3}`},
		{"bad hidden id", NSFeed, `{"hidden_item_ids":["../etc"]}`},
		{"bad unpaid policy", NSPlatform, `{"unpaid_policy":"maybe"}`},
		{"goal out of range", NSPlatform, `{"daily_goal_default":9}`},
		{"copy too long", NSCopy, `{"home_tip":{"fr":"` + strings.Repeat("x", 201) + `","en":"ok"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Set(context.Background(), tc.ns, []byte(tc.body), "admin", nil)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want ValidationError, got %v", err)
			}
		})
	}
	if _, err := s.Set(context.Background(), "nope", []byte(`{}`), "admin", nil); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown namespace: %v", err)
	}
	if d, _ := s.Current(context.Background(), NSMusic); d.Version != 0 {
		t.Fatal("a rejected save must not create a version")
	}
}

func TestSetWritesAuditAndVersions(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	d1, err := s.Set(ctx, NSMusic, []byte(`{"default_volume":40}`), "alice", nil)
	if err != nil || d1.Version != 1 {
		t.Fatalf("v1: %v %+v", err, d1)
	}
	d2, err := s.Set(ctx, NSMusic, []byte(`{"default_volume":55,"enabled":false}`), "bob", nil)
	if err != nil || d2.Version != 2 {
		t.Fatalf("v2: %v %+v", err, d2)
	}
	vs, err := s.Versions(ctx, NSMusic, 10)
	if err != nil || len(vs) != 2 {
		t.Fatalf("versions: %v %+v", err, vs)
	}
	top := vs[0] // newest first: who, when, old and new are all there
	if top.Actor != "bob" || top.At == "" || !strings.Contains(string(top.OldValue), `"default_volume":40`) || !strings.Contains(string(top.Value), `"default_volume":55`) {
		t.Fatalf("history row: %+v", top)
	}
	if string(vs[1].OldValue) != "null" {
		t.Fatalf("first version has no old value: %s", vs[1].OldValue)
	}
	var n int
	if err := db.R.QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action = 'appconfig.set' AND target_id = 'music'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("admin_audit rows: %d %v", n, err)
	}
	if _, err := db.W.Exec(`UPDATE app_config SET actor = 'x'`); err == nil {
		t.Fatal("app_config must be append-only")
	}
}

func TestConflict(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	zero, one := 0, 1
	if _, err := s.Set(ctx, NSFeed, []byte(`{}`), "a", &zero); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set(ctx, NSFeed, []byte(`{}`), "b", &zero); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale editor must conflict: %v", err)
	}
	if _, err := s.Set(ctx, NSFeed, []byte(`{}`), "b", &one); err != nil {
		t.Fatal(err)
	}
}

func TestRollback(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.Rollback(ctx, NSMusic, "a", nil); !errors.Is(err, ErrNothingToRevert) {
		t.Fatalf("nothing saved: %v", err)
	}
	mustSet := func(st *Store, body string) {
		t.Helper()
		if _, err := st.Set(ctx, NSMusic, []byte(body), "a", nil); err != nil {
			t.Fatal(err)
		}
	}
	mustSet(s, `{"default_volume":40}`)
	mustSet(s, `{"default_volume":80}`)
	d, err := s.Rollback(ctx, NSMusic, "carol", nil)
	if err != nil || d.Version != 3 || Get[Music](ctx, s, NSMusic).DefaultVolume != 40 {
		t.Fatalf("rollback: %v %+v", err, d)
	}
	vs, _ := s.Versions(ctx, NSMusic, 10)
	if vs[0].Action != "rollback" || vs[0].Actor != "carol" || len(vs) != 3 {
		t.Fatalf("history keeps the rollback: %+v", vs[0])
	}
	// rolling back the very first save restores the defaults
	s2, _ := newStore(t)
	mustSet(s2, `{"default_volume":99}`)
	if _, err := s2.Rollback(ctx, NSMusic, "a", nil); err != nil || Get[Music](ctx, s2, NSMusic).DefaultVolume != 30 {
		t.Fatalf("rollback to defaults: %v", err)
	}
	to := 1
	if d, err := s.Rollback(ctx, NSMusic, "a", &to); err != nil || Get[Music](ctx, s, NSMusic).DefaultVolume != 40 || d.Version != 4 {
		t.Fatalf("to_version: %v", err)
	}
	bad := 99
	var ve *ValidationError
	if _, err := s.Rollback(ctx, NSMusic, "a", &bad); !errors.As(err, &ve) {
		t.Fatalf("missing version: %v", err)
	}
}

func TestCacheInvalidation(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	if Get[Music](ctx, s, NSMusic).DefaultVolume != 30 {
		t.Fatal("default")
	}
	// prove reads are cached: a row written behind the back of the store stays invisible until invalidation
	if _, err := db.W.Exec(`INSERT INTO app_config (ns,version,value,action,actor) VALUES ('music',1,'{"default_volume":77}','set','sql')`); err != nil {
		t.Fatal(err)
	}
	if Get[Music](ctx, s, NSMusic).DefaultVolume != 30 {
		t.Fatal("expected the cached default before invalidation")
	}
	s.invalidate()
	if Get[Music](ctx, s, NSMusic).DefaultVolume != 77 {
		t.Fatal("invalidate must drop the cache")
	}
	// a normal Set is visible at once, public document included
	pub1, _ := s.PublicJSON(ctx)
	if _, err := s.Set(ctx, NSMusic, []byte(`{"default_volume":10}`), "a", nil); err != nil {
		t.Fatal(err)
	}
	if Get[Music](ctx, s, NSMusic).DefaultVolume != 10 {
		t.Fatal("Set must invalidate")
	}
	pub2, _ := s.PublicJSON(ctx)
	if string(pub1) == string(pub2) || !strings.Contains(string(pub2), `"default_volume":10`) {
		t.Fatal("public document must be rebuilt after a save")
	}
}

func TestPublicHidesDisabledAndHiddenItems(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.Set(ctx, NSMusic, []byte(`{"tracks":[{"id":"on","title":{"fr":"A","en":"A"},"enabled":true},{"id":"off","title":{"fr":"B","en":"B"},"enabled":false}]}`), "a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set(ctx, NSFeed, []byte(`{"hidden_item_ids":["secret-item"]}`), "a", nil); err != nil {
		t.Fatal(err)
	}
	raw, err := s.PublicJSON(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatal("public document is not JSON")
	}
	body := string(raw)
	if strings.Contains(body, `"off"`) || strings.Contains(body, "secret-item") || !strings.Contains(body, `"on"`) {
		t.Fatalf("public leak or gap: %s", body)
	}
}
