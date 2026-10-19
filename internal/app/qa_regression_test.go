package app

// Regression tests for QA-BE-001 (size caps), QA-BE-002 (auth before validation), QA-BE-003 (idempotent replay vs rate limit).
//
// Go for Spring people: t.Run makes a named sub-test (JUnit @ParameterizedTest row);
// httptest.NewRecorder is MockMvc: it records the response without opening a socket.

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

var qaPathParam = regexp.MustCompile(`\{[^}]+\}`) // {childId} -> placeholder

// qaRaw sends a raw body (not JSON-encoded by the helper) and returns the recorder.
func qaRaw(h http.Handler, method, path, bearer, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// QA-BE-002: with no token, a protected operation answers 401 even when the body or a parameter is invalid.
func TestQAAuthBeforeValidationEveryOperation(t *testing.T) {
	h, _ := newTestServer(t)
	for id, m := range httpapi.Ops {
		t.Run(id, func(t *testing.T) {
			path := "/v1" + qaPathParam.ReplaceAllString(m.Path, "x") // invalid uuid on purpose: parameter errors must not beat auth
			rec := qaRaw(h, m.Method, path, "", `{"bad`, nil)         // broken JSON, no token, no Idempotency-Key
			if m.Public {
				if rec.Code >= 500 && rec.Code != http.StatusNotImplemented { // public ops may answer 401 themselves (bad device cookie)
					t.Fatalf("public op: status %d", rec.Code)
				}
				return
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// QA-BE-002: a token with the wrong role gets 403 before validation too.
func TestQAWrongRoleBeforeValidation(t *testing.T) {
	f := newFeedFixture(t, true)
	rec := qaRaw(f.h, "POST", "/v1/children", f.childTok, `{"bad`, nil) // CreateChild is parent-only
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}

// QA-BE-001: oversized bodies and fields are refused, argon2 never sees them.
func TestQASizeCaps(t *testing.T) {
	h, _ := newTestServer(t)
	ok := func(email, pw string) string {
		return `{"email":"` + email + `","password":"` + pw + `","locale":"fr-CA"}`
	}
	t.Run("2MB body is 413", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/register", "", ok("big@b.co", strings.Repeat("x", 2_000_000)), nil)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d, want 413", rec.Code)
		}
	})
	t.Run("129 char password is 422", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/register", "", ok("p129@b.co", strings.Repeat("x", 129)), nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422", rec.Code)
		}
	})
	t.Run("128 char password is accepted", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/register", "", ok("max128@b.co", strings.Repeat("x", 128)), nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d, want 201", rec.Code)
		}
	})
	t.Run("login long password is 422", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/login", "", `{"email":"a@b.co","password":"`+strings.Repeat("x", 5000)+`"}`, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422", rec.Code)
		}
	})
	t.Run("long email is 422", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/login", "", `{"email":"`+strings.Repeat("a", 300)+`@b.co","password":"whatever-long-enough"}`, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422", rec.Code)
		}
	})
	t.Run("delete account long password is 422", func(t *testing.T) {
		f := newFeedFixture(t, true)
		rec := qaRaw(f.h, "DELETE", "/v1/account", f.parentTok, `{"password":"`+strings.Repeat("x", 5000)+`"}`, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422", rec.Code)
		}
	})
	t.Run("unauthenticated 2MB body is 401 not 413", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/children", "", strings.Repeat("x", 2_000_000), nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401", rec.Code)
		}
	})
}

// QA-BE-003: a replay with the same Idempotency-Key returns the stored result, not 429.
func TestQAIdempotentReplayBypassesRateLimit(t *testing.T) {
	f := newFeedFixture(t, true)
	start := func(key string) (int, map[string]any) {
		code, out, _ := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, `{"world":"nature","kind":"chapter"}`, map[string]string{"Idempotency-Key": key})
		return code, out
	}
	key := uuid.NewString()
	code, first := start(key)
	if code != 201 {
		t.Fatalf("first start: %d", code)
	}
	saw429 := false
	for i := 0; i < 12; i++ { // StartSession allows 10 per minute per child
		if c, _ := start(uuid.NewString()); c == 429 {
			saw429 = true
		}
	}
	if !saw429 {
		t.Fatal("limiter never tripped, test is not testing anything")
	}
	code, again := start(key)
	if code != 201 || again["id"] != first["id"] {
		t.Fatalf("replay: %d id %v, want 201 id %v", code, again["id"], first["id"])
	}
	if c, _ := start(uuid.NewString()); c != 429 { // a fresh key is still limited
		t.Fatalf("fresh key after the limit: %d, want 429", c)
	}
}

// QA-BE-003 for answers: the replay of an answered beat survives an exhausted per-session limit.
func TestQAAnswerReplayBypassesRateLimit(t *testing.T) {
	f := newFeedFixture(t, true)
	sid := f.startChapter(t, "")
	code, beat, raw := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
	if code != 201 {
		t.Fatalf("next beat: %d %s", code, raw)
	}
	beatID := beat["id"].(string)
	key := uuid.NewString()
	c1, first := f.answer(t, sid, beatID, 1, key)
	if c1 != 200 {
		t.Fatalf("first answer: %d %v", c1, first)
	}
	saw429 := false
	for i := 0; i < 35; i++ { // AnswerBeat allows 30 per minute per session
		if c, _ := f.answer(t, sid, beatID, 1, uuid.NewString()); c == 429 {
			saw429 = true
		}
	}
	if !saw429 {
		t.Fatal("limiter never tripped")
	}
	c2, again := f.answer(t, sid, beatID, 1, key)
	if c2 != 200 || again["outcome"] != first["outcome"] {
		t.Fatalf("replay: %d %v, want 200 %v", c2, again, first)
	}
}
