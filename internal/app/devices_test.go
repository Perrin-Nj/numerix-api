package app

// Device session tests: the whole HTTP stack on a temp SQLite database (Spring: MockMvc + @DataJpaTest).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dvCall sends a JSON request with an optional bearer token and nx_device cookie and returns the recorder.
func dvCall(t *testing.T, h http.Handler, method, path, bearer, cookie string, body any) *httptest.ResponseRecorder {
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
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: deviceCookie, Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// dvJSON decodes a recorder body into a map.
func dvJSON(rec *httptest.ResponseRecorder) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return m
}

// dvCookie returns the nx_device cookie set by a response (nil when absent).
func dvCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == deviceCookie {
			return c
		}
	}
	return nil
}

// dvRegister registers a parent and returns (access token, device cookie value).
func dvRegister(t *testing.T, h http.Handler, email string) (string, string) {
	t.Helper()
	rec := dvCall(t, h, "POST", "/v1/auth/register", "", "", map[string]any{"email": email, "password": "correct-horse-battery", "locale": "fr-CA"})
	if rec.Code != 201 {
		t.Fatalf("register %d %s", rec.Code, rec.Body)
	}
	c := dvCookie(rec)
	if c == nil {
		t.Fatal("register set no nx_device cookie")
	}
	return plStr(dvJSON(rec), "access_token"), c.Value
}

func TestDeviceCookieLifecycle(t *testing.T) {
	h, srv := newTestServer(t)
	srv.Config.Env = "test" // not development, so the cookie must be Secure

	tok, c1 := dvRegister(t, h, "dev@example.com")

	// cookie attributes
	rec := dvCall(t, h, "POST", "/v1/auth/login", "", c1, map[string]any{"email": "dev@example.com", "password": "correct-horse-battery"})
	ck := dvCookie(rec) // login on a known device rotates its cookie, no second device
	if ck == nil || !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" || ck.MaxAge != 180*24*3600 {
		t.Fatalf("cookie attributes %+v", ck)
	}
	c1 = ck.Value

	// known device: rotates the cookie and returns a session
	rec = dvCall(t, h, "POST", "/v1/auth/device", "", c1, nil)
	if rec.Code != 200 || plStr(dvJSON(rec), "access_token") == "" || plStr(dvJSON(rec), "refresh_token") == "" {
		t.Fatalf("device login %d %s", rec.Code, rec.Body)
	}
	c2 := dvCookie(rec)
	if c2 == nil || c2.Value == c1 {
		t.Fatal("cookie was not rotated")
	}
	// the old value is dead and the browser is told to delete it
	rec = dvCall(t, h, "POST", "/v1/auth/device", "", c1, nil)
	if rec.Code != 401 {
		t.Fatalf("reused cookie %d", rec.Code)
	}
	if gone := dvCookie(rec); gone == nil || gone.MaxAge >= 0 {
		t.Fatalf("dead cookie not cleared %+v", gone)
	}
	// no cookie at all
	if rec = dvCall(t, h, "POST", "/v1/auth/device", "", "", nil); rec.Code != 401 {
		t.Fatalf("no cookie %d", rec.Code)
	}

	// list: one device, marked current when its cookie is sent
	rec = dvCall(t, h, "GET", "/v1/account/devices", tok, c2.Value, nil)
	items, _ := dvJSON(rec)["items"].([]any)
	if rec.Code != 200 || len(items) != 1 {
		t.Fatalf("list %d %s", rec.Code, rec.Body)
	}
	d := items[0].(map[string]any)
	if d["current"] != true || d["label"] != "Chrome on Windows" || d["user_agent_family"] != "Chrome" {
		t.Fatalf("device %v", d)
	}
	// without parent token: 401
	if rec = dvCall(t, h, "GET", "/v1/account/devices", "", "", nil); rec.Code != 401 {
		t.Fatalf("anonymous list %d", rec.Code)
	}

	// another parent cannot see or revoke it
	tok2, _ := dvRegister(t, h, "other@example.com")
	if rec = dvCall(t, h, "DELETE", "/v1/account/devices/"+d["id"].(string), tok2, "", nil); rec.Code != 404 {
		t.Fatalf("foreign revoke %d", rec.Code)
	}

	// revoke: cookie stops working
	if rec = dvCall(t, h, "DELETE", "/v1/account/devices/"+d["id"].(string), tok, c2.Value, nil); rec.Code != 204 {
		t.Fatalf("revoke %d %s", rec.Code, rec.Body)
	}
	if rec = dvCall(t, h, "POST", "/v1/auth/device", "", c2.Value, nil); rec.Code != 401 {
		t.Fatalf("revoked cookie %d", rec.Code)
	}
	if rec = dvCall(t, h, "DELETE", "/v1/account/devices/"+d["id"].(string), tok, "", nil); rec.Code != 404 {
		t.Fatalf("second revoke %d", rec.Code)
	}
}

func TestLogoutRevokesCurrentDevice(t *testing.T) {
	h, _ := newTestServer(t)
	tok, c1 := dvRegister(t, h, "out@example.com")
	rec := dvCall(t, h, "POST", "/v1/auth/logout", tok, c1, map[string]any{})
	if rec.Code != 204 {
		t.Fatalf("logout %d", rec.Code)
	}
	if rec = dvCall(t, h, "POST", "/v1/auth/device", "", c1, nil); rec.Code != 401 {
		t.Fatalf("cookie after logout %d", rec.Code)
	}
}

func TestChildPINSkippedOnlyOnRememberedDevice(t *testing.T) {
	h, _ := newTestServer(t)
	tok, cookie := dvRegister(t, h, "kid@example.com")
	rec := dvCall(t, h, "POST", "/v1/children", tok, "", map[string]any{"display_name": "Zoé", "grade": 3, "avatar_id": "fox", "pin": "1234"})
	if rec.Code != 201 {
		t.Fatalf("create child %d %s", rec.Code, rec.Body)
	}
	kid := plStr(dvJSON(rec), "id")
	if rec = dvCall(t, h, "POST", "/v1/auth/consent", tok, "", map[string]any{"child_id": kid, "version": "2026-10", "accepted": true}); rec.Code != 204 {
		t.Fatalf("consent %d", rec.Code)
	}
	_, otherCookie := dvRegister(t, h, "stranger@example.com")

	settings := map[string]any{"daily_goal_chapters": 1, "enabled_worlds": []string{"nature"}, "read_aloud": true, "sound": true, "remember_on_device": false}
	cases := []struct {
		name     string
		remember bool
		cookie   string
		want     int
	}{
		{"setting off, valid cookie: PIN still needed", false, cookie, 403},
		{"setting on, no cookie: PIN needed", true, "", 403},
		{"setting on, cookie of another parent: PIN needed", true, otherCookie, 403},
		{"setting on, garbage cookie: PIN needed", true, "not-a-token", 403},
		{"setting on, valid cookie: no PIN", true, cookie, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			settings["remember_on_device"] = c.remember
			if rec := dvCall(t, h, "PUT", "/v1/children/"+kid+"/settings", tok, "", settings); rec.Code != 200 {
				t.Fatalf("settings %d %s", rec.Code, rec.Body)
			}
			rec := dvCall(t, h, "POST", "/v1/children/"+kid+"/token", tok, c.cookie, map[string]any{})
			if rec.Code != c.want {
				t.Fatalf("got %d want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
	// a wrong PIN still fails and a right PIN still works with no cookie
	if rec = dvCall(t, h, "POST", "/v1/children/"+kid+"/token", tok, "", map[string]any{"pin": "1234"}); rec.Code != 200 {
		t.Fatalf("right PIN %d", rec.Code)
	}
}

func TestDevUserAgent(t *testing.T) {
	cases := []struct{ ua, family, label string }{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0 Safari/537.36", "Chrome", "Chrome on Windows"},
		{"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/126.0 Safari/537.36 Edg/126.0", "Edge", "Edge on Windows"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 Version/17.5 Mobile/15E148 Safari/604.1", "Safari", "Safari on iOS"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0", "Firefox", "Firefox on Linux"},
		{"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/126.0 Mobile Safari/537.36", "Chrome", "Chrome on Android"},
		{"curl/8.0", "Other", "Other"},
		{"", "Other", "Other"},
	}
	for _, c := range cases {
		if f, l := devUserAgent(c.ua); f != c.family || l != c.label {
			t.Errorf("%q: got (%s, %s) want (%s, %s)", c.ua, f, l, c.family, c.label)
		}
	}
}
