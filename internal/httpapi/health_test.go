package httpapi

import (
	"net/http"
	"net/http/httptest" // fake request/response objects for tests (Spring: MockMvc)
	"strings"
	"testing" // standard test framework (Spring: JUnit); files ending in _test.go are test-only
)

// TestHealth checks that GET /health answers 200 with {"status":"ok"}.
// Test functions start with "Test" and take *testing.T (JUnit: @Test method + assertions).
func TestHealth(t *testing.T) {
	// Table-driven test: a list of cases, each run as its own sub-test (JUnit: @ParameterizedTest).
	cases := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{"get works", http.MethodGet, http.StatusOK},
		{"post is refused", http.MethodPost, http.StatusMethodNotAllowed}, // the "GET " prefix in the route does this
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, "/health", nil) // build a fake request
			rec := httptest.NewRecorder()                        // records what the handler writes

			NewRouter().ServeHTTP(rec, req) // run the real router against the fake request

			if rec.Code != c.wantStatus { // plain if instead of assertEquals
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if c.wantStatus == http.StatusOK && !strings.Contains(rec.Body.String(), `"status":"ok"`) {
				t.Fatalf("body = %q, want it to contain status ok", rec.Body.String())
			}
		})
	}
}
