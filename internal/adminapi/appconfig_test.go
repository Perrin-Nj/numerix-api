package adminapi

// Tests for the runtime config endpoints: auth before validation, validation, audit, rollback, conflict,
// read-only environment view (Spring: a MockMvc suite).

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

func setupCfg(t *testing.T) (http.Handler, *appconfig.Store) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := appconfig.New(db, false)
	env := map[string]string{"JWT_SECRET": "super-secret-value-do-not-leak-0123456789", "PORT": "8260"}
	h := New(Deps{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Token: tok, Now: time.Now, Cfg: cfg,
		Getenv: func(k string) string { return env[k] }})
	return h, cfg
}

func TestAppConfigAuthBeforeValidation(t *testing.T) {
	h, cfg := setupCfg(t)
	bad := `{"value":{"default_volume":999}}` // would be a 422 with a token
	for _, auth := range []string{"", "wrong-token"} {
		for _, req := range [][2]string{{"PUT", "/admin/v1/appconfig/music"}, {"PUT", "/admin/v1/appconfig/nope"}, {"POST", "/admin/v1/appconfig/music/rollback"}, {"GET", "/admin/v1/appconfig"}} {
			if c, _, _ := call(h, req[0], req[1], bad, auth); c != 401 {
				t.Fatalf("%s %s auth=%q: want 401 before validation, got %d", req[0], req[1], auth, c)
			}
		}
	}
	if d, _ := cfg.Current(context.Background(), "music"); d.Version != 0 {
		t.Fatal("an unauthenticated request must not write")
	}
	if c, _, _ := call(h, "PUT", "/admin/v1/appconfig/music", bad, tok); c != 422 {
		t.Fatalf("authenticated invalid body: want 422, got %d", c)
	}
	if c, _, _ := call(h, "PUT", "/admin/v1/appconfig/nope", `{"value":{}}`, tok); c != 404 {
		t.Fatalf("unknown namespace: want 404, got %d", c)
	}
}

func TestAppConfigPutAuditRollback(t *testing.T) {
	h, _ := setupCfg(t)
	c, m, _ := call(h, "PUT", "/admin/v1/appconfig/maintenance", `{"value":{"enabled":true,"level":"warning","message":{"fr":"Entretien ce soir","en":"Maintenance tonight"},"block_play":false},"expected_version":0}`, tok, "X-Admin-Actor", "alice")
	if c != 200 || m["version"] != 1.0 || m["updated_by"] != "alice" {
		t.Fatalf("put: %d %v", c, m)
	}
	if c, _, _ := call(h, "PUT", "/admin/v1/appconfig/maintenance", `{"value":{"level":"info"},"expected_version":0}`, tok); c != 409 {
		t.Fatalf("stale expected_version: want 409, got %d", c)
	}
	call(h, "PUT", "/admin/v1/appconfig/maintenance", `{"value":{"level":"info"}}`, tok, "X-Admin-Actor", "bob")
	_, g, _ := call(h, "GET", "/admin/v1/appconfig/maintenance", "", tok)
	if g["version"] != 2.0 || g["defaults"] == nil {
		t.Fatalf("get: %v", g)
	}
	c, r, _ := call(h, "POST", "/admin/v1/appconfig/maintenance/rollback", `{}`, tok, "X-Admin-Actor", "carol")
	if c != 200 || r["version"] != 3.0 {
		t.Fatalf("rollback: %d %v", c, r)
	}
	_, g, _ = call(h, "GET", "/admin/v1/appconfig/maintenance", "", tok)
	if v := g["value"].(map[string]any); v["enabled"] != true || v["level"] != "warning" {
		t.Fatalf("rollback must restore the first save: %v", v)
	}
	_, vs, _ := call(h, "GET", "/admin/v1/appconfig/maintenance/versions", "", tok)
	items := vs["items"].([]any)
	if len(items) != 3 || items[0].(map[string]any)["actor"] != "carol" || items[0].(map[string]any)["action"] != "rollback" {
		t.Fatalf("versions: %v", items)
	}
	_, au, _ := call(h, "GET", "/admin/v1/audit?page_size=50", "", tok)
	actions := ""
	for _, it := range au["items"].([]any) {
		actions += it.(map[string]any)["action"].(string) + " "
	}
	if !strings.Contains(actions, "appconfig.set") || !strings.Contains(actions, "appconfig.rollback") {
		t.Fatalf("audit rows missing: %s", actions)
	}
}

func TestAppConfigEnvIsReadOnlyAndHidesSecrets(t *testing.T) {
	h, _ := setupCfg(t)
	req := httptestGet(h, "/admin/v1/appconfig")
	if strings.Contains(req, "super-secret-value") {
		t.Fatalf("a secret leaked: %s", req)
	}
	if !strings.Contains(req, `"key":"JWT_SECRET","set":true,"secret":true`) || !strings.Contains(req, `"key":"PORT","set":true,"secret":false,"value":"8260"`) {
		t.Fatalf("env view: %s", req)
	}
}

func TestLegacyConfigEndpointUsesTheStore(t *testing.T) {
	h, cfg := setupCfg(t)
	if c, _, _ := call(h, "PUT", "/admin/v1/config", `{"unpaid_policy":"maybe"}`, tok); c != 422 {
		t.Fatalf("bad value: %d", c)
	}
	if c, _, _ := call(h, "PUT", "/admin/v1/config", `{"unpaid_policy":"allow"}`, tok); c != 200 {
		t.Fatalf("put: %d", c)
	}
	// the game reads the same store, so the edit reaches it at once
	if appconfig.Get[appconfig.Platform](context.Background(), cfg, appconfig.NSPlatform).UnpaidPolicy != "allow" {
		t.Fatal("PUT /config did not reach the store the game reads")
	}
}

func TestAIDraftSwitch(t *testing.T) {
	h, _ := setupCfg(t)
	call(h, "PUT", "/admin/v1/appconfig/ai", `{"value":{"draft_enabled":false,"narrator_enabled":true}}`, tok)
	if c, m, _ := call(h, "POST", "/admin/v1/questions/ai-draft", `{}`, tok); c != 503 || m["code"] != "ai_disabled" {
		t.Fatalf("switched off: %d %v", c, m)
	}
}

// httptestGet returns the raw body of an authenticated GET.
func httptestGet(h http.Handler, path string) string {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}
