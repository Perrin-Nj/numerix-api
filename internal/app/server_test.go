package app

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

	"github.com/Perrin-Nj/numerix-api/internal/config"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// newTestServer builds the full HTTP stack on a temp database (Spring: @SpringBootTest + MockMvc).
func newTestServer(t *testing.T) (http.Handler, *Server) {
	t.Helper()
	cfg, err := config.Load(func(string) string { return "" }) // all defaults
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(db, cfg, log)
	return httpapi.NewHandler(httpapi.Deps{Impl: s, Config: cfg, Log: log, Issuer: s.Issuer}), s
}

// do sends one request through the handler and returns the recorded response.
func do(h http.Handler, method, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}")) // empty JSON object satisfies required-body decoding
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthAndReady(t *testing.T) {
	h, _ := newTestServer(t)
	for _, path := range []string{"/v1/health", "/v1/ready"} {
		rec := do(h, "GET", path, "")
		if rec.Code != 200 {
			t.Fatalf("%s: status %d body %s", path, rec.Code, rec.Body)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["status"] != "ok" {
			t.Fatalf("%s: body %s", path, rec.Body)
		}
	}
}

func TestRequestIDHeader(t *testing.T) {
	h, _ := newTestServer(t)
	if id := do(h, "GET", "/v1/health", "").Header().Get("X-Request-Id"); len(id) < 8 {
		t.Fatalf("generated id missing: %q", id)
	}
	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") != "abc-123" {
		t.Fatal("valid inbound id must be echoed")
	}
	req.Header.Set("X-Request-Id", "bad id\nx")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); got == "bad id\nx" || got == "" {
		t.Fatalf("unsafe id must be replaced, got %q", got)
	}
}

func TestAuthRules(t *testing.T) {
	h, s := newTestServer(t)
	parent, _, _ := s.Issuer.IssueParent("p1")
	child, _, _ := s.Issuer.IssueChild("p1", "c1")
	tests := []struct {
		name, method, path, token string
		wantStatus                int
		wantCode                  string
	}{
		{"no token", "GET", "/v1/account", "", 401, "unauthorized"},
		{"garbage token", "GET", "/v1/account", "garbage", 401, "unauthorized"},
		{"parent reaches stub", "GET", "/v1/account", parent, 501, "not_implemented"},
		{"child on parent route", "GET", "/v1/account", child, 403, "forbidden"},
		{"public stub needs no token", "POST", "/v1/auth/login", "", 501, "not_implemented"},
		{"unknown route", "GET", "/nope", "", 404, "not_found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(h, tc.method, tc.path, tc.token)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d, body %s", rec.Code, tc.wantStatus, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("content type %q", ct)
			}
			var p map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &p)
			if p["code"] != tc.wantCode || int(p["status"].(float64)) != tc.wantStatus || p["type"] == "" || p["title"] == "" {
				t.Fatalf("bad problem body: %s", rec.Body)
			}
		})
	}
}

func TestRateLimited429(t *testing.T) {
	h, _ := newTestServer(t)
	// POST /auth/login is limited to 10 per minute per IP (the stub answers 501 until the limit hits).
	for i := 0; i < 10; i++ {
		if rec := do(h, "POST", "/v1/auth/login", ""); rec.Code != 501 {
			t.Fatalf("request %d: status %d", i+1, rec.Code)
		}
	}
	rec := do(h, "POST", "/v1/auth/login", "")
	if rec.Code != 429 {
		t.Fatalf("11th request: status %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header missing")
	}
	var p struct {
		Code              string `json:"code"`
		RetryAfterSeconds int    `json:"retry_after_seconds"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p.Code != "rate_limited" || p.RetryAfterSeconds < 1 {
		t.Fatalf("bad body: %s", rec.Body)
	}
	// Health is never throttled.
	if rec := do(h, "GET", "/v1/health", ""); rec.Code != 200 {
		t.Fatalf("health status %d", rec.Code)
	}
}

func TestSecurityHeadersAndRecover(t *testing.T) {
	h, _ := newTestServer(t)
	rec := do(h, "GET", "/v1/health", "")
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("security headers missing")
	}
	// A panicking handler becomes a 500 problem instead of killing the connection.
	boom := httpapi.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		httpapi.Recover(slog.New(slog.NewTextHandler(io.Discard, nil))))
	rec = httptest.NewRecorder()
	boom.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 {
		t.Fatalf("panic status %d", rec.Code)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	h := httpapi.ConcurrencyLimit(1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
	}))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/x", nil))
	<-entered // first request holds the only slot
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/x", nil))
	close(release)
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("saturated: status %d retry-after %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}
