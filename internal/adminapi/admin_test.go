package adminapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

const tok = "test-admin-token-123456"

// setup opens a temp database, seeds two parents and one child, and returns the handler.
func setup(t *testing.T) http.Handler {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	seed := []string{
		`INSERT INTO parents (id,email,password_hash,name,locale) VALUES ('p1','a@x.ca','h','Alice','fr-CA'),('p2','b@x.ca','h','Bob','en-CA')`,
		`INSERT INTO subscriptions (id,parent_id,plan_id,status,trial_ends_at) VALUES ('s1','p1','free','trial','2999-01-01T00:00:00.000Z')`,
		`INSERT INTO children (id,parent_id,display_name,grade) VALUES ('c1','p1','Zoe',3)`,
		`INSERT INTO sessions (id,child_id,status,world_id,json) VALUES ('se1','c1','finished','nature','{"correct":3,"total":3}')`,
	}
	for _, q := range seed {
		if _, err := db.W.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return New(Deps{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Token: tok, Origins: "https://admin.example", Now: time.Now})
}

// call sends one request and decodes the JSON answer into a map (nil for empty bodies).
func call(h http.Handler, method, path, body, auth string, hdr ...string) (int, map[string]any, http.Header) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m, rec.Header()
}

func TestAuthAndCORS(t *testing.T) {
	h := setup(t)
	tests := []struct {
		name, method, path, token string
		want                      int
	}{
		{"no token", "GET", "/admin/v1/overview", "", 401},
		{"wrong token", "GET", "/admin/v1/overview", "nope", 401},
		{"good token", "GET", "/admin/v1/overview", tok, 200},
		{"unknown route", "GET", "/admin/v1/zzz", tok, 404},
		{"preflight needs no token", "OPTIONS", "/admin/v1/parents", "", 204},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, _, _ := call(h, tc.method, tc.path, "", tc.token); got != tc.want {
				t.Fatalf("status %d, want %d", got, tc.want)
			}
		})
	}
	_, _, hd := call(h, "GET", "/admin/v1/overview", "", tok, "Origin", "https://admin.example")
	if hd.Get("Access-Control-Allow-Origin") != "https://admin.example" {
		t.Fatal("allowed origin must get CORS header")
	}
	_, _, hd = call(h, "GET", "/admin/v1/overview", "", tok, "Origin", "https://evil.example")
	if hd.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("other origins must not get CORS header")
	}
}

func TestParentsFlow(t *testing.T) {
	h := setup(t)
	if c, m, _ := call(h, "GET", "/admin/v1/parents?q=alice", "", tok); c != 200 || m["total"] != 1.0 {
		t.Fatalf("list: %d %v", c, m)
	}
	patches := []struct {
		name, body string
		want       int
	}{
		{"bad locale", `{"locale":"xx"}`, 422},
		{"bad plan", `{"subscription":{"plan":"gold"}}`, 422},
		{"bad json", `{`, 400},
		{"block", `{"status":"blocked","blocked_reason":"abuse"}`, 200},
		{"mark paid", `{"subscription":{"status":"paid"}}`, 200},
		{"extend", `{"subscription":{"extend_days":10}}`, 200},
	}
	for _, tc := range patches {
		t.Run(tc.name, func(t *testing.T) {
			if c, _, _ := call(h, "PATCH", "/admin/v1/parents/p1", tc.body, tok); c != tc.want {
				t.Fatalf("status %d, want %d", c, tc.want)
			}
		})
	}
	_, m, _ := call(h, "GET", "/admin/v1/parents/p1", "", tok)
	sub := m["subscription"].(map[string]any)
	if m["status"] != "blocked" || m["blocked_reason"] != "abuse" || sub["status"] != "active" || sub["plan"] != "family_monthly" || sub["marked_by_admin"] != true {
		t.Fatalf("unexpected parent: %v", m)
	}
	if len(m["children"].([]any)) != 1 {
		t.Fatal("detail must list the child")
	}
	// parent without subscription row gets one created by an override
	if c, m, _ := call(h, "PATCH", "/admin/v1/parents/p2", `{"subscription":{"status":"active","plan":"family_yearly"}}`, tok); c != 200 || m["subscription"].(map[string]any)["status"] != "active" {
		t.Fatalf("p2 override: %d %v", c, m)
	}
	if c, _, _ := call(h, "DELETE", "/admin/v1/parents/p2", "", tok); c != 204 {
		t.Fatalf("delete: %d", c)
	}
	if c, _, _ := call(h, "GET", "/admin/v1/parents/p2", "", tok); c != 404 {
		t.Fatalf("deleted parent: %d", c)
	}
}

func TestChildrenAndAudit(t *testing.T) {
	h := setup(t)
	if c, m, _ := call(h, "GET", "/admin/v1/children?q=zo", "", tok); c != 200 || m["total"] != 1.0 {
		t.Fatalf("list: %d %v", c, m)
	}
	c, m, _ := call(h, "GET", "/admin/v1/children/c1", "", tok)
	if c != 200 || m["sessions_total"] != 1.0 || m["parent"].(map[string]any)["email"] != "a@x.ca" {
		t.Fatalf("detail: %d %v", c, m)
	}
	if c, m, _ := call(h, "GET", "/admin/v1/children/c1/activity", "", tok); c != 200 || len(m["sessions"].([]any)) != 1 {
		t.Fatalf("activity: %d %v", c, m)
	}
	if c, _, _ := call(h, "PATCH", "/admin/v1/children/c1", `{"grade":9}`, tok); c != 422 {
		t.Fatalf("bad grade: %d", c)
	}
	c, m, _ = call(h, "PATCH", "/admin/v1/children/c1", `{"display_name":"Zed","settings":{"sound":false},"daily_time_limit_minutes":30}`, tok, "X-Admin-Actor", "root")
	if s := m["settings"].(map[string]any); c != 200 || m["display_name"] != "Zed" || s["sound"] != false || s["daily_time_limit_minutes"] != 30.0 {
		t.Fatalf("patch: %d %v", c, m)
	}
	// audit holds the 3 reads and the update, newest first, with the actor
	_, m, _ = call(h, "GET", "/admin/v1/audit", "", tok)
	items := m["items"].([]any)
	if m["total"] != 4.0 || items[0].(map[string]any)["actor"] != "root" || items[0].(map[string]any)["action"] != "child.update" {
		t.Fatalf("audit: %v", m)
	}
}

func TestPlansConfigNotifications(t *testing.T) {
	h := setup(t)
	if c, _, _ := call(h, "PUT", "/admin/v1/config", `{"unpaid_policy":"maybe"}`, tok); c != 422 {
		t.Fatalf("bad config: %d", c)
	}
	if c, m, _ := call(h, "PUT", "/admin/v1/config", `{"unpaid_policy":"allow","features":{"feed":false}}`, tok); c != 200 || m["unpaid_policy"] != "allow" || m["trial_days"] != 14.0 {
		t.Fatalf("config: %d %v", c, m)
	}
	_, m, _ := call(h, "GET", "/admin/v1/subscriptions/plans", "", tok)
	plans := m["plans"].([]any)
	if len(plans) != 3 {
		t.Fatalf("default plans: %v", m)
	}
	if c, _, _ := call(h, "PUT", "/admin/v1/subscriptions/plans", `{"plans":[]}`, tok); c != 422 {
		t.Fatalf("bad plans: %d", c)
	}
	plans[1].(map[string]any)["price_cad_cents"] = 999.0
	body, _ := json.Marshal(map[string]any{"plans": plans})
	if c, m, _ := call(h, "PUT", "/admin/v1/subscriptions/plans", string(body), tok); c != 200 || m["plans"].([]any)[1].(map[string]any)["price_cad_cents"] != 999.0 {
		t.Fatalf("plans: %d %v", c, m)
	}
	if c, _, _ := call(h, "POST", "/admin/v1/notifications/broadcast", `{"title":"","body":""}`, tok); c != 422 {
		t.Fatalf("empty broadcast: %d", c)
	}
	c, m, _ := call(h, "POST", "/admin/v1/notifications/broadcast", `{"title":{"fr":"Salut","en":"Hi"},"body":"Msg","filter":{"locale":"en-CA"}}`, tok)
	if c != 201 || m["recipients"] != 1.0 {
		t.Fatalf("broadcast: %d %v", c, m)
	}
	_, m, _ = call(h, "GET", "/admin/v1/notifications", "", tok)
	row := m["items"].([]any)[0].(map[string]any)
	if m["total"] != 1.0 || row["title"] != "Hi" || row["broadcast"] != true {
		t.Fatalf("notifications: %v", m)
	}
}

func TestOverviewAndHealth(t *testing.T) {
	h := setup(t)
	_, m, _ := call(h, "GET", "/admin/v1/overview", "", tok)
	by := m["by_subscription"].(map[string]any)
	if m["parents_total"] != 2.0 || m["kids_total"] != 1.0 || by["trial"] != 1.0 || by["expired"] != 1.0 || len(m["trend_30"].([]any)) != 31 || len(m["activity_by_day_90"].([]any)) != 90 {
		t.Fatalf("overview: %v", m)
	}
	if c, m, _ := call(h, "GET", "/admin/v1/health", "", tok); c != 200 || m["db"] != "ok" {
		t.Fatalf("health: %d %v", c, m)
	}
}

func TestAuditIsAppendOnly(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.W.Exec(`INSERT INTO admin_audit (id,actor,action,target_type,target_id) VALUES ('1','a','x','y','z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`DELETE FROM admin_audit`); err == nil {
		t.Fatal("delete must be refused")
	}
	if _, err := db.W.Exec(`UPDATE admin_audit SET actor='b'`); err == nil {
		t.Fatal("update must be refused")
	}
}
