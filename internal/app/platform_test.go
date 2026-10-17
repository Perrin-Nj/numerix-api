package app

// Platform flow tests, like Spring MockMvc tests: the whole HTTP stack on a temp SQLite database.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// plCall sends a JSON request and decodes the JSON answer into a map (nil for empty bodies).
func plCall(t *testing.T, h http.Handler, method, path, bearer string, body any) (int, map[string]any, http.Header) {
	t.Helper()
	raw := "{}"
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(b)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Header()
}

// plWant fails the test when the status differs; it prints the body to help.
func plWant(t *testing.T, got int, body map[string]any, want int, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: status %d, want %d, body %v", label, got, want, body)
	}
}

func plStr(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func TestPlatformFullFlow(t *testing.T) {
	h, _ := newTestServer(t)
	pw := "correct-horse-battery"

	// register: ok, duplicate (any case), weak password
	code, reg, _ := plCall(t, h, "POST", "/v1/auth/register", "", map[string]any{"email": "Ana@Example.com", "password": pw, "locale": "fr-CA"})
	plWant(t, code, reg, 201, "register")
	code, body, _ := plCall(t, h, "POST", "/v1/auth/register", "", map[string]any{"email": "ana@example.com", "password": pw, "locale": "fr-CA"})
	plWant(t, code, body, 409, "duplicate register")
	if plStr(body, "code") != "email_taken" {
		t.Fatalf("code %v", body)
	}
	code, body, _ = plCall(t, h, "POST", "/v1/auth/register", "", map[string]any{"email": "b@example.com", "password": "short", "locale": "fr-CA"})
	plWant(t, code, body, 422, "weak password")

	// login: ok, wrong password, unknown email
	code, login, _ := plCall(t, h, "POST", "/v1/auth/login", "", map[string]any{"email": "ana@example.com", "password": pw})
	plWant(t, code, login, 200, "login")
	for _, c := range []map[string]any{{"email": "ana@example.com", "password": "nope-nope-nope"}, {"email": "ghost@example.com", "password": pw}} {
		code, body, _ = plCall(t, h, "POST", "/v1/auth/login", "", c)
		plWant(t, code, body, 401, "bad login")
	}

	// refresh rotates; the old token then revokes the family
	old := plStr(login, "refresh_token")
	code, rot, _ := plCall(t, h, "POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": old})
	plWant(t, code, rot, 200, "refresh")
	code, body, _ = plCall(t, h, "POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": old})
	plWant(t, code, body, 401, "refresh reuse")
	code, body, _ = plCall(t, h, "POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": plStr(rot, "refresh_token")})
	plWant(t, code, body, 401, "family revoked")

	tok := plStr(login, "access_token")

	// account
	code, acc, _ := plCall(t, h, "GET", "/v1/account", tok, nil)
	plWant(t, code, acc, 200, "get account")
	code, acc, _ = plCall(t, h, "PATCH", "/v1/account", tok, map[string]any{"name": "Ana", "locale": "en-CA"})
	plWant(t, code, acc, 200, "patch account")
	if acc["name"] != "Ana" || acc["locale"] != "en-CA" {
		t.Fatalf("account %v", acc)
	}
	code, body, _ = plCall(t, h, "PATCH", "/v1/account", tok, map[string]any{"locale": "de-DE"})
	plWant(t, code, body, 422, "bad locale")

	// children: validation table
	for _, c := range []struct {
		name string
		in   map[string]any
	}{
		{"empty name", map[string]any{"display_name": " ", "grade": 2, "avatar_id": "fox"}},
		{"grade 9", map[string]any{"display_name": "Zoé", "grade": 9, "avatar_id": "fox"}},
		{"pin letters", map[string]any{"display_name": "Zoé", "grade": 2, "avatar_id": "fox", "pin": "12ab"}},
		{"bad zone", map[string]any{"display_name": "Zoé", "grade": 2, "avatar_id": "fox", "timezone": "Mars/Base"}},
	} {
		code, body, _ = plCall(t, h, "POST", "/v1/children", tok, c.in)
		plWant(t, code, body, 422, c.name)
	}
	code, kid, _ := plCall(t, h, "POST", "/v1/children", tok, map[string]any{"display_name": "Zoé", "grade": 3, "avatar_id": "fox", "pin": "1234"})
	plWant(t, code, kid, 201, "create child")
	if kid["has_pin"] != true || kid["consent_given"] != false {
		t.Fatalf("child %v", kid)
	}
	if strings.Contains(plJSON(kid), "pin_hash") || strings.Contains(plJSON(kid), "argon2") {
		t.Fatal("pin leaked")
	}
	kidID := plStr(kid, "id")
	code, nopin, _ := plCall(t, h, "POST", "/v1/children", tok, map[string]any{"display_name": "Max", "grade": 1, "avatar_id": "bear"})
	plWant(t, code, nopin, 201, "create second child")
	code, list, _ := plCall(t, h, "GET", "/v1/children", tok, nil)
	plWant(t, code, list, 200, "list")
	if n := len(list["items"].([]any)); n != 2 {
		t.Fatalf("items %d", n)
	}

	// token: consent first, then wrong PIN x5, lockout
	code, body, _ = plCall(t, h, "POST", "/v1/children/"+kidID+"/token", tok, map[string]any{"pin": "1234"})
	plWant(t, code, body, 403, "no consent")
	if plStr(body, "code") != "consent_missing" {
		t.Fatalf("code %v", body)
	}
	code, body, _ = plCall(t, h, "POST", "/v1/auth/consent", tok, map[string]any{"child_id": kidID, "version": "2026-10", "accepted": true})
	plWant(t, code, body, 204, "consent")
	code, body, _ = plCall(t, h, "POST", "/v1/auth/consent", tok, map[string]any{"child_id": kidID, "version": "2026-10", "accepted": true})
	plWant(t, code, body, 204, "consent twice")
	code, body, _ = plCall(t, h, "POST", "/v1/auth/consent", tok, map[string]any{"child_id": "99999999-9999-4999-8999-999999999999", "version": "2026-10", "accepted": true})
	plWant(t, code, body, 404, "consent for a stranger")
	for i := 0; i < 5; i++ {
		code, body, _ = plCall(t, h, "POST", "/v1/children/"+kidID+"/token", tok, map[string]any{"pin": "0000"})
		plWant(t, code, body, 403, "wrong pin")
		if plStr(body, "code") != "invalid_pin" {
			t.Fatalf("code %v", body)
		}
	}
	code, body, hdr := plCall(t, h, "POST", "/v1/children/"+kidID+"/token", tok, map[string]any{"pin": "1234"})
	plWant(t, code, body, 429, "locked even with the right pin")
	if hdr.Get("Retry-After") == "" || body["retry_after_seconds"] == nil {
		t.Fatalf("retry hints missing: %v %v", hdr, body)
	}

	// second child (no PIN, consent given) gets a token; the child token reads settings but cannot write
	plCall(t, h, "POST", "/v1/auth/consent", tok, map[string]any{"child_id": plStr(nopin, "id"), "version": "2026-10", "accepted": true})
	code, ct, _ := plCall(t, h, "POST", "/v1/children/"+plStr(nopin, "id")+"/token", tok, nil)
	plWant(t, code, ct, 200, "child token")
	childTok, maxID := plStr(ct, "access_token"), plStr(nopin, "id")
	code, set, _ := plCall(t, h, "GET", "/v1/children/"+maxID+"/settings", childTok, nil)
	plWant(t, code, set, 200, "child reads settings")
	code, body, _ = plCall(t, h, "PUT", "/v1/children/"+maxID+"/settings", childTok, set)
	plWant(t, code, body, 403, "child writes settings")
	code, body, _ = plCall(t, h, "GET", "/v1/children/"+kidID, childTok, nil)
	plWant(t, code, body, 403, "child reads a sibling")

	// settings validation and round trip
	bad := map[string]any{"daily_goal_chapters": 9, "enabled_worlds": []string{"nature"}, "read_aloud": true, "sound": true}
	code, body, _ = plCall(t, h, "PUT", "/v1/children/"+kidID+"/settings", tok, bad)
	plWant(t, code, body, 422, "bad settings")
	good := map[string]any{"daily_goal_chapters": 2, "daily_time_limit_minutes": 30, "enabled_worlds": []string{"nature", "espace"}, "read_aloud": false, "sound": true, "kid_can_change_language": false}
	code, body, _ = plCall(t, h, "PUT", "/v1/children/"+kidID+"/settings", tok, good)
	plWant(t, code, body, 200, "put settings")
	code, body, _ = plCall(t, h, "GET", "/v1/children/"+kidID+"/settings", tok, nil)
	plWant(t, code, body, 200, "get settings")
	if body["kid_can_change_language"] != false || body["daily_goal_chapters"] != float64(2) {
		t.Fatalf("settings %v", body)
	}

	// update child: rename, remove pin
	code, body, _ = plCall(t, h, "PATCH", "/v1/children/"+kidID, tok, map[string]any{"display_name": "Zoé B", "onboarding_completed": true, "pin": ""})
	plWant(t, code, body, 200, "patch child")
	if body["display_name"] != "Zoé B" || body["has_pin"] != false || body["onboarding_completed"] != true {
		t.Fatalf("child %v", body)
	}

	// subscription: trial, instant dev checkout, cancel (twice), invalid plan
	code, sub, _ := plCall(t, h, "GET", "/v1/subscription", tok, nil)
	plWant(t, code, sub, 200, "subscription")
	if sub["status"] != "trial" || sub["can_play"] != true {
		t.Fatalf("sub %v", sub)
	}
	code, body, _ = plCall(t, h, "POST", "/v1/subscription/checkout", tok, map[string]any{"plan": "free", "method": "card"})
	plWant(t, code, body, 422, "free plan checkout")
	code, sub, _ = plCall(t, h, "POST", "/v1/subscription/checkout", tok, map[string]any{"plan": "family_monthly", "method": "card"})
	plWant(t, code, sub, 200, "checkout")
	if sub["status"] != "active" || sub["plan"] != "family_monthly" || sub["method"] != "card" {
		t.Fatalf("sub %v", sub)
	}
	for i := 0; i < 2; i++ {
		code, sub, _ = plCall(t, h, "POST", "/v1/subscription/cancel", tok, nil)
		plWant(t, code, sub, 200, "cancel")
	}
	if sub["status"] != "canceled" || sub["can_play"] != true {
		t.Fatalf("sub %v", sub)
	}
	code, plans, _ := plCall(t, h, "GET", "/v1/subscription/plans", tok, nil)
	plWant(t, code, plans, 200, "plans")
	code, pub, _ := plCall(t, h, "GET", "/v1/platform/public", "", nil)
	plWant(t, code, pub, 200, "platform public")
	if pub["unpaid_policy"] != "block" {
		t.Fatalf("public %v", pub)
	}

	// notifications, push, events
	code, notes, _ := plCall(t, h, "GET", "/v1/notifications", tok, nil)
	plWant(t, code, notes, 200, "notifications")
	code, body, _ = plCall(t, h, "POST", "/v1/notifications/nope/read", tok, nil)
	plWant(t, code, body, 404, "unknown notification")
	push := map[string]any{"endpoint": "https://push.example.com/abc", "keys": map[string]any{"p256dh": "k1", "auth": "k2"}}
	code, body, _ = plCall(t, h, "POST", "/v1/push/subscribe", tok, push)
	plWant(t, code, body, 201, "push new")
	code, body, _ = plCall(t, h, "POST", "/v1/push/subscribe", tok, push)
	plWant(t, code, body, 204, "push known")
	code, body, _ = plCall(t, h, "DELETE", "/v1/push/subscribe", tok, map[string]any{"endpoint": "https://push.example.com/abc"})
	plWant(t, code, body, 204, "push delete")
	code, body, _ = plCall(t, h, "POST", "/v1/events", childTok, map[string]any{"events": []any{
		map[string]any{"name": "world_picker_opened", "at": "2026-10-12T14:00:00Z", "props": map[string]any{"world": "nature"}},
		map[string]any{"name": "Bad Name!", "at": "2026-10-12T14:00:00Z"},
	}})
	plWant(t, code, body, 202, "events")

	// export has the child and never the PIN hash
	code, exp, _ := plCall(t, h, "GET", "/v1/children/"+kidID+"/export", tok, nil)
	plWant(t, code, exp, 200, "export")
	if exp["format_version"] != float64(1) || exp["child"] == nil || exp["consent"] == nil || strings.Contains(plJSON(exp), "argon2") {
		t.Fatalf("export %v", exp)
	}

	// delete child, then it is gone
	code, body, _ = plCall(t, h, "DELETE", "/v1/children/"+kidID, tok, nil)
	plWant(t, code, body, 204, "delete child")
	code, body, _ = plCall(t, h, "GET", "/v1/children/"+kidID, tok, nil)
	plWant(t, code, body, 404, "deleted child")

	// logout is idempotent
	code, body, _ = plCall(t, h, "POST", "/v1/auth/logout", tok, map[string]any{"refresh_token": "unknown"})
	plWant(t, code, body, 204, "logout")

	// delete account: wrong password 401, right password 204, login refused, email free again
	code, body, _ = plCall(t, h, "DELETE", "/v1/account", tok, map[string]any{"password": "wrong-wrong-wrong"})
	plWant(t, code, body, 401, "delete wrong password")
	code, body, _ = plCall(t, h, "DELETE", "/v1/account", tok, map[string]any{"password": pw})
	plWant(t, code, body, 204, "delete account")
	code, body, _ = plCall(t, h, "POST", "/v1/auth/login", "", map[string]any{"email": "ana@example.com", "password": pw})
	plWant(t, code, body, 401, "login after delete")
	code, body, _ = plCall(t, h, "GET", "/v1/account", tok, nil)
	plWant(t, code, body, 401, "old token after delete")
	code, body, _ = plCall(t, h, "POST", "/v1/auth/register", "", map[string]any{"email": "ana@example.com", "password": pw, "locale": "fr-CA"})
	plWant(t, code, body, 201, "register again")
}

func plJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// TestPlatformAccessRules checks the guard and ownership rules around the new operations.
func TestPlatformAccessRules(t *testing.T) {
	h, _ := newTestServer(t)
	pw := "correct-horse-battery"
	mk := func(email string) (string, string) {
		_, r, _ := plCall(t, h, "POST", "/v1/auth/register", "", map[string]any{"email": email, "password": pw, "locale": "en-CA"})
		_, c, _ := plCall(t, h, "POST", "/v1/children", plStr(r, "access_token"), map[string]any{"display_name": "K", "grade": 1, "avatar_id": "owl"})
		return plStr(r, "access_token"), plStr(c, "id")
	}
	a, childA := mk("a@example.com")
	b, _ := mk("b@example.com")
	for _, c := range []struct {
		name, method, path, tok string
		want                    int
	}{
		{"no token", "GET", "/v1/children", "", 401},
		{"other parent get", "GET", "/v1/children/" + childA, b, 404},
		{"other parent delete", "DELETE", "/v1/children/" + childA, b, 404},
		{"other parent export", "GET", "/v1/children/" + childA + "/export", b, 404},
		{"owner get", "GET", "/v1/children/" + childA, a, 200},
	} {
		code, body, _ := plCall(t, h, c.method, c.path, c.tok, nil)
		plWant(t, code, body, c.want, c.name)
	}
	// ninth child is refused
	for i := 0; i < 7; i++ {
		plCall(t, h, "POST", "/v1/children", a, map[string]any{"display_name": "K", "grade": 1, "avatar_id": "owl"})
	}
	code, body, _ := plCall(t, h, "POST", "/v1/children", a, map[string]any{"display_name": "K", "grade": 1, "avatar_id": "owl"})
	plWant(t, code, body, 422, "ninth child")
}
