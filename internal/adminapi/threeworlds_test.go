package adminapi

// T018/T019/T021 admin side: world status switch, free access grant, child settings against the world registry.
// The admin API has one credential (the admin bearer token). Every other bearer, including a parent or child JWT,
// is refused before any handler runs: 401 on this API (the main API answers 403 for a wrong role).

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// newRoutes lists every route added by this release, with a valid body.
var newRoutes = []struct{ method, path, body string }{
	{"PUT", "/admin/v1/worlds/jeux/status", `{"status":"draft"}`},
	{"PUT", "/admin/v1/parents/p1/free-access", `{"until":null,"reason":"pilot"}`},
	{"DELETE", "/admin/v1/parents/p1/free-access", ``},
}

func TestNewAdminRoutesRefuseEveryOtherCredential(t *testing.T) {
	h := setup(t)
	parentJWT := "eyJhbGciOiJIUzI1NiJ9.eyJyb2xlIjoicGFyZW50In0.c2ln"
	childJWT := "eyJhbGciOiJIUzI1NiJ9.eyJyb2xlIjoiY2hpbGQifQ.c2ln"
	for _, r := range newRoutes {
		for name, cred := range map[string]string{"no token": "", "parent token": parentJWT, "child token": childJWT, "wrong token": "nope"} {
			if c, _, _ := call(h, r.method, r.path, r.body, cred); c != 401 && c != 403 {
				t.Errorf("%s %s with %s: %d, want 401 or 403", r.method, r.path, name, c)
			}
		}
	}
	// nothing changed
	_, m, _ := call(h, "GET", "/admin/v1/audit", "", tok)
	for _, it := range m["items"].([]any) {
		if a := it.(map[string]any)["action"].(string); strings.HasPrefix(a, "world.") || strings.HasPrefix(a, "parent.free") {
			t.Fatalf("a refused call left an audit row: %v", it)
		}
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestWorldStatusSwitch(t *testing.T) {
	h := setup(t)
	if c, m, _ := call(h, "PUT", "/admin/v1/worlds/jeux/status", `{"status":"draft"}`, tok, "X-Admin-Actor", "root"); c != 200 || m["status"] != "draft" {
		t.Fatalf("draft: %d %v", c, m)
	}
	if c, m, _ := call(h, "PUT", "/admin/v1/worlds/jeux/status", `{"status":"draft"}`, tok); c != 200 || m["status"] != "draft" {
		t.Fatalf("setting the same status again is fine: %d %v", c, m)
	}
	if c, m, _ := call(h, "PUT", "/admin/v1/worlds/jeux/status", `{"status":"published"}`, tok); c != 200 || m["status"] != "published" {
		t.Fatalf("publish: %d %v", c, m)
	}
	for body, want := range map[string]int{`{"status":"retired"}`: 422, `{"status":"x"}`: 422, `{}`: 422} {
		if c, _, _ := call(h, "PUT", "/admin/v1/worlds/jeux/status", body, tok); c != want {
			t.Errorf("%s: %d, want %d", body, c, want)
		}
	}
	if c, _, _ := call(h, "PUT", "/admin/v1/worlds/atlantis/status", `{"status":"draft"}`, tok); c != 404 {
		t.Fatalf("unknown world: %d", c)
	}
	if c, m, _ := call(h, "PUT", "/admin/v1/worlds/echecs/status", `{"status":"draft"}`, tok); c != 422 {
		t.Fatalf("a default world cannot go draft: %d %v", c, m)
	}
	_, m, _ := call(h, "GET", "/admin/v1/audit", "", tok)
	found := false
	for _, it := range m["items"].([]any) {
		row := it.(map[string]any)
		if row["action"] == "world.status" && row["target_id"] == "jeux" && row["actor"] == "root" && strings.Contains(row["detail"].(string), "published -> draft") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no audit row with before and after: %v", m)
	}
	// the world list shows the status
	_, wl, _ := call(h, "GET", "/admin/v1/worlds", "", tok)
	if !strings.Contains(toJSON(wl), `"status":"published"`) {
		t.Fatalf("worlds list: %v", wl)
	}
}

func TestChildSettingsUseTheWorldRegistry(t *testing.T) {
	h := setup(t)
	if c, _, _ := call(h, "PATCH", "/admin/v1/children/c1", `{"settings":{"enabled_worlds":["ocean","echecs"]}}`, tok); c != 200 {
		t.Fatalf("any published world is accepted by the admin: %d", c)
	}
	call(h, "PUT", "/admin/v1/worlds/jeux/status", `{"status":"draft"}`, tok)
	if c, _, _ := call(h, "PATCH", "/admin/v1/children/c1", `{"settings":{"enabled_worlds":["jeux"]}}`, tok); c != 422 {
		t.Fatalf("a draft world is refused: %d", c)
	}
	if c, _, _ := call(h, "PATCH", "/admin/v1/children/c1", `{"settings":{"enabled_worlds":["atlantis"]}}`, tok); c != 422 {
		t.Fatalf("an unknown world is refused: %d", c)
	}
}

func TestFreeAccessGrantExtendEnd(t *testing.T) {
	h := setup(t)
	until := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	c, m, _ := call(h, "PUT", "/admin/v1/parents/p1/free-access", `{"until":"`+until+`","reason":"pilot family"}`, tok, "X-Admin-Actor", "root")
	if c != 200 {
		t.Fatalf("grant: %d %v", c, m)
	}
	fa := m["subscription"].(map[string]any)["free_access"].(map[string]any)
	if fa["until"] != until || fa["reason"] != "pilot family" || fa["granted_by"] != "root" || m["subscription"].(map[string]any)["can_play"] != true {
		t.Fatalf("free access view: %v", m["subscription"])
	}
	// extend, then open ended
	if c, _, _ := call(h, "PUT", "/admin/v1/parents/p1/free-access", `{"until":null,"reason":"founder"}`, tok); c != 200 {
		t.Fatalf("open ended: %d", c)
	}
	// bad bodies
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	for body, want := range map[string]int{
		`{"until":"` + past + `","reason":"x"}`:                      422,
		`{"until":"tomorrow","reason":"x"}`:                          422,
		`{"until":null}`:                                             422,
		`{"until":null,"reason":""}`:                                 422,
		`{"until":null,"reason":"` + strings.Repeat("a", 201) + `"}`: 422,
		`{"until":null,"reason":"x","extra":1}`:                      422,
	} {
		if c, _, _ := call(h, "PUT", "/admin/v1/parents/p1/free-access", body, tok); c != want {
			t.Errorf("%s: %d, want %d", body, c, want)
		}
	}
	if c, _, _ := call(h, "PUT", "/admin/v1/parents/nobody/free-access", `{"until":null,"reason":"x"}`, tok); c != 404 {
		t.Fatalf("unknown parent: %d", c)
	}
	// end it
	if c, _, _ := call(h, "DELETE", "/admin/v1/parents/p1/free-access", "", tok); c != 200 && c != 204 {
		t.Fatalf("end: %d", c)
	}
	_, p, _ := call(h, "GET", "/admin/v1/parents/p1", "", tok)
	if _, has := p["subscription"].(map[string]any)["free_access"]; has {
		t.Fatalf("free access must be gone: %v", p["subscription"])
	}
	// audit: grant, extend, end, each with the actor
	_, a, _ := call(h, "GET", "/admin/v1/audit?page_size=100", "", tok)
	n := 0
	for _, it := range a["items"].([]any) {
		if it.(map[string]any)["action"] == "parent.free_access" && it.(map[string]any)["target_id"] == "p1" {
			n++
		}
	}
	if n < 3 {
		t.Fatalf("want 3 audit rows (grant, extend, end), got %d", n)
	}
}

func TestAdminPlansUseTheNeutralYearlyText(t *testing.T) {
	h := setup(t)
	_, m, _ := call(h, "GET", "/admin/v1/subscriptions/plans", "", tok)
	s := toJSON(m)
	if !strings.Contains(s, "Save with the yearly plan") || strings.Contains(s, "Two months free") || !strings.Contains(s, "family_yearly") {
		t.Fatalf("plans: %s", s)
	}
}
