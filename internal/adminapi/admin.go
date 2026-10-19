// Package adminapi is the platform admin API served under /admin/v1 (not part of the OpenAPI contract).
// Endpoints are documented in docs/ADMIN_API.md.
//
// Go for Spring people: API is one "controller + service" for the admin app. Routing uses the Go 1.22
// ServeMux patterns ("GET /admin/v1/parents/{id}"), like @GetMapping("/parents/{id}"). Auth and CORS are one
// wrapper function (Spring: a OncePerRequestFilter), applied once around the whole router.
package adminapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/drafter"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
	"github.com/Perrin-Nj/numerix-api/internal/store"
	"github.com/google/uuid"
)

// Deps are the constructor arguments (Spring: constructor injection).
type Deps struct {
	Store      *store.Store
	Log        *slog.Logger
	Token      string // ADMIN_API_TOKEN; empty outside production falls back to a dev token, empty in production = disabled
	Production bool
	Origins    string                 // ADMIN_ORIGIN: comma separated browser origins allowed by CORS; nothing else gets CORS headers
	Now        func() time.Time       // clock, replaced in tests; nil = time.Now
	Sync       func() mongosync.Stats // Mongo sync counters for /admin/v1/health; nil = not reported
	Content    *contentdb.Repo        // the question bank repository, shared with the game so its cache is invalidated; nil = a private one
	Assets     *assetstore.Store      // image store; nil = a private one
	Drafter    drafter.Provider       // AI draft provider; nil = POST /questions/ai-draft answers 503 not_configured
}

// API holds the dependencies shared by every handler method.
type API struct {
	db      *store.Store
	log     *slog.Logger
	tokenH  [32]byte // SHA-256 of the token: comparing fixed-size hashes keeps the compare constant-time
	enabled bool
	origins []string
	now     func() time.Time
	sync    func() mongosync.Stats

	content *contentdb.Repo
	assets  *assetstore.Store
	drafter drafter.Provider
	drafts  draftLimiter // AI draft rate limit per admin
}

// tsLayout matches SQLite's strftime('%Y-%m-%dT%H:%M:%fZ') so text comparisons and sorting stay valid.
const tsLayout = "2006-01-02T15:04:05.000Z"

// New builds the handler to mount at /admin/v1/ (Spring: building the controller bean + filter chain).
func New(d Deps) http.Handler {
	a := &API{db: d.Store, log: d.Log, now: d.Now, sync: d.Sync, content: d.Content, assets: d.Assets, drafter: d.Drafter}
	if a.content == nil {
		a.content = contentdb.New(d.Store, nil)
	}
	if a.assets == nil {
		a.assets = assetstore.New(d.Store)
	}
	if a.now == nil {
		a.now = time.Now
	}
	token := d.Token
	if token == "" && !d.Production {
		token = "dev-admin-token" // same dev default as the JS mock; never active in production
	}
	if token != "" {
		a.tokenH, a.enabled = sha256.Sum256([]byte(token)), true
	}
	for _, o := range strings.Split(d.Origins, ",") { // keep only non-empty origins
		if o = strings.TrimSpace(o); o != "" {
			a.origins = append(a.origins, o)
		}
	}

	mux := http.NewServeMux() // the router
	// overview, health
	mux.HandleFunc("GET /admin/v1/overview", a.overview)
	mux.HandleFunc("GET /admin/v1/health", a.health)
	// parents
	mux.HandleFunc("GET /admin/v1/parents", a.listParents)
	mux.HandleFunc("GET /admin/v1/parents/{id}", a.getParent)
	mux.HandleFunc("PATCH /admin/v1/parents/{id}", a.patchParent)
	mux.HandleFunc("DELETE /admin/v1/parents/{id}", a.deleteParent)
	// children
	mux.HandleFunc("GET /admin/v1/children", a.listChildren)
	mux.HandleFunc("GET /admin/v1/children/{id}", a.getChild)
	mux.HandleFunc("PATCH /admin/v1/children/{id}", a.patchChild)
	mux.HandleFunc("GET /admin/v1/children/{id}/activity", a.childActivity)
	// plans and config
	mux.HandleFunc("GET /admin/v1/subscriptions/plans", a.getPlans)
	mux.HandleFunc("PUT /admin/v1/subscriptions/plans", a.putPlans)
	mux.HandleFunc("GET /admin/v1/config", a.getConfig)
	mux.HandleFunc("PUT /admin/v1/config", a.putConfig)
	// notifications and audit
	mux.HandleFunc("GET /admin/v1/notifications", a.listBroadcasts)
	mux.HandleFunc("POST /admin/v1/notifications/broadcast", a.broadcast)
	mux.HandleFunc("GET /admin/v1/audit", a.listAudit)
	a.contentRoutes(mux)
	// anything else under /admin/v1/ answers a JSON 404 (after auth)
	mux.HandleFunc("/admin/v1/", func(w http.ResponseWriter, r *http.Request) { httpapi.WriteError(w, r, httpapi.ErrNotFound) })

	return a.guard(mux)
}

// guard applies CORS, the preflight answer and the bearer check before the router runs.
func (a *API) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" && a.originAllowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Headers", "authorization,content-type,x-admin-actor")
			h.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions { // preflight carries no credentials
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !a.enabled {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusServiceUnavailable, "admin_disabled", "ADMIN_API_TOKEN is not set."))
			return
		}
		given := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		gh := sha256.Sum256([]byte(given))
		if given == "" || subtle.ConstantTimeCompare(gh[:], a.tokenH[:]) != 1 { // constant time compare
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "unauthorized", "A valid admin bearer token is required."))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB cap on request bodies
		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether origin is listed in ADMIN_ORIGIN.
func (a *API) originAllowed(origin string) bool {
	for _, o := range a.origins {
		if o == origin {
			return true
		}
	}
	return false
}

// ---- small helpers shared by all handlers ----

// writeJSON sends v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // headers already sent: an encode error cannot be reported
}

// fail turns err into problem+json; unexpected errors are logged and shown as a generic 500.
func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *httpapi.APIError
	if !errors.As(err, &ae) {
		a.log.Error("admin api error", "path", r.URL.Path, "err", err)
	}
	httpapi.WriteError(w, r, err)
}

// bad builds a 422 validation error with a readable sentence.
func bad(msg string) error {
	return httpapi.NewError(http.StatusUnprocessableEntity, "validation_failed", msg)
}

// decode reads the JSON body into v; an empty body leaves v untouched.
func decode(r *http.Request, v any) error {
	err := json.NewDecoder(r.Body).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		return httpapi.NewError(http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
	}
	return nil
}

// actor is the admin name written to the audit log (header X-Admin-Actor, default "admin").
func actor(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Admin-Actor")); v != "" {
		return v[:min(len(v), 80)]
	}
	return "admin"
}

// page reads page and page_size (default 20, max 100) and returns the SQL offset too.
func page(r *http.Request) (pg, size, offset int) {
	size, _ = strconv.Atoi(r.URL.Query().Get("page_size"))
	pg, _ = strconv.Atoi(r.URL.Query().Get("page"))
	size = min(100, max(1, orDefault(size, 20)))
	pg = max(1, pg)
	return pg, size, (pg - 1) * size
}

// orDefault returns def when v is zero.
func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// pageOf is the paged answer shape the admin app expects.
type pageOf[T any] struct {
	Items    []T `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

// paged slices an in-memory list into one page (used where filtering happens in Go).
func paged[T any](all []T, r *http.Request) pageOf[T] {
	pg, size, off := page(r)
	items := []T{} // never null in JSON
	if off < len(all) {
		items = all[off:min(len(all), off+size)]
	}
	return pageOf[T]{Items: items, Page: pg, PageSize: size, Total: len(all)}
}

// writeAudit appends one audit row inside the caller's transaction (so the change and its audit commit together).
func (a *API) writeAudit(tx *sql.Tx, who, action, targetType, targetID, detail string) error {
	if len(detail) > 300 {
		detail = detail[:300]
	}
	_, err := tx.Exec(`INSERT INTO admin_audit (id, at, actor, action, target_type, target_id, detail) VALUES (?,?,?,?,?,?,?)`,
		uuid.NewString(), a.stamp(), who, action, targetType, targetID, detail)
	return err
}

// auditRead records a read of child data (its own small write transaction).
func (a *API) auditRead(ctx context.Context, r *http.Request, action, targetType, targetID, detail string) error {
	return a.db.WithWriteTx(ctx, func(tx *sql.Tx) error {
		return a.writeAudit(tx, actor(r), action, targetType, targetID, detail)
	})
}

// stamp returns the current UTC time in the stored text format.
func (a *API) stamp() string { return a.now().UTC().Format(tsLayout) }
