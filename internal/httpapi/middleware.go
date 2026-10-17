package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// Middleware is a function that wraps a handler (Spring: a servlet Filter).
type Middleware func(http.Handler) http.Handler

// Chain applies middleware so the FIRST one listed is the OUTERMOST (runs first on the way in).
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- { // wrap from the inside out
		h = mws[i](h)
	}
	return h
}

// ---- request id ----

type requestIDKey struct{}

// RequestID returns the id of the current request ("" outside a request).
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// validRequestID accepts a caller-supplied id only if it is short and made of safe characters,
// so it can never inject newlines into logs or headers.
func validRequestID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// WithRequestID gives every request an id (header X-Request-Id), reuses a valid inbound one,
// stores it in the context for logs, and echoes it in the response (Spring: MDC + a correlation filter).
func WithRequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if !validRequestID(id) {
				b := make([]byte, 8)
				_, _ = rand.Read(b)
				id = hex.EncodeToString(b)
			}
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
		})
	}
}

// ---- access log ----

// statusRecorder remembers the status code and byte count written by the inner handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK // Write without WriteHeader means 200
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush keeps Server-Sent Events working through the wrapper.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// AccessLog logs one line per request. It logs the route PATTERN (e.g. /children/{childId}), never
// the query string, headers, body, emails, names or answers (Law 25).
func AccessLog(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			route := r.Pattern // set by the mux after routing; empty if nothing matched
			if route == "" {
				route = "unmatched"
			}
			log.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("request_id", RequestID(r.Context())),
				slog.String("route", route),
				slog.String("method", r.Method),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}

// ---- panic recovery ----

// Recover turns a panic in a handler into a 500 problem and logs the stack (Spring: a global exception handler).
func Recover(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					if p == http.ErrAbortHandler { // net/http uses this panic on purpose; keep it
						panic(p)
					}
					log.Error("panic", "request_id", RequestID(r.Context()), "panic", p, "stack", string(debug.Stack()))
					WriteError(w, r, NewError(http.StatusInternalServerError, "internal_error", ""))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ---- security headers ----

// SecurityHeaders adds defensive headers to every response. The API returns JSON only, so the CSP is strict.
func SecurityHeaders(production bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Cross-Origin-Resource-Policy", "same-site")
			if h.Get("Cache-Control") == "" {
				h.Set("Cache-Control", "no-store") // personal data must not be cached by intermediaries
			}
			if production {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---- CORS ----

// CORS allows only the listed origins (admin app). Other origins get no CORS headers, so browsers block them.
// The web app never calls the backend from the browser (contract section 17), so the list is usually empty.
func CORS(allowed []string) Middleware {
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := set[origin]; ok && origin != "" {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" { // preflight
					h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
					h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, Accept-Language")
					h.Set("Access-Control-Max-Age", "600")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---- request deadline ----

// Deadline gives each request a context that expires after d, so slow work stops instead of piling up.
// Server-Sent Events (narration stream) are exempt because they are meant to stay open.
func Deadline(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel() // always release the timer
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ---- global concurrency limit ----

// ConcurrencyLimit allows at most n requests in flight. Extra requests get an immediate 429 with
// Retry-After instead of queueing (queues turn overload into timeouts). Health checks are exempt.
func ConcurrencyLimit(n int) Middleware {
	slots := make(chan struct{}, n) // a buffered channel used as a counting semaphore
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/health") || strings.HasSuffix(r.URL.Path, "/ready") {
				next.ServeHTTP(w, r)
				return
			}
			select {
			case slots <- struct{}{}: // got a slot
				defer func() { <-slots }() // free it when the request ends
				next.ServeHTTP(w, r)
			default: // all slots busy
				WriteError(w, r, RateLimited(1)) // sets Retry-After header and body field
			}
		})
	}
}
