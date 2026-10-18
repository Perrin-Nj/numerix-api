package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/config"
)

// Deps is everything NewHandler needs (Spring: the beans a @Configuration class wires together).
type Deps struct {
	Impl   api.StrictServerInterface // the business implementation (app.Server)
	Config config.Config
	Log    *slog.Logger
	Issuer *auth.Issuer
	// Limiter may be nil; a fresh one is created. Tests pass their own to control state.
	Limiter *Limiter
}

// NewHandler assembles the full HTTP stack, outermost first:
// request id -> access log -> panic recovery -> security headers -> CORS -> concurrency limit -> per-IP limit
// -> request deadline -> token parsing -> generated router -> per-operation guard -> your handler.
func NewHandler(d Deps) http.Handler {
	lim := d.Limiter
	if lim == nil {
		lim = NewLimiter()
	}
	cfg := d.Config

	// Strict handler: oapi-codegen decodes the request into typed structs and encodes the typed response.
	// The two error hooks route every failure through WriteError so the format is always problem+json.
	strict := api.NewStrictHandlerWithOptions(d.Impl, []api.StrictMiddlewareFunc{Guard(lim, cfg.TrustProxy)}, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			// malformed JSON / bad body: the contract has one client-error code, validation_failed (422).
			WriteError(w, r, Validation(FieldError{Field: "body", Message: "invalid request body"}))
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			if _, ok := err.(*APIError); !ok { // unexpected failure: log the cause, hide it from the client
				d.Log.Error("handler error", "request_id", RequestID(r.Context()), "err", err)
			}
			WriteError(w, r, err)
		},
	})

	mux := http.NewServeMux()
	// Register every contract route under /v1. ErrorHandlerFunc handles bad path/query parameters.
	api.HandlerWithOptions(strict, api.StdHTTPServerOptions{
		BaseURL:    "/v1",
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			WriteError(w, r, Validation(FieldError{Field: "parameters", Message: "invalid parameter"}))
		},
	})
	// Anything else: problem+json 404, never an HTML page.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { WriteError(w, r, ErrNotFound) })

	return Chain(mux,
		WithRequestID(),
		AccessLog(d.Log),
		Recover(d.Log),
		SecurityHeaders(cfg.IsProduction()),
		CORS(cfg.AllowedOrigins),
		ConcurrencyLimit(cfg.MaxConcurrent),
		IPLimit(lim, cfg.RateLimitIPPerMin, cfg.TrustProxy),
		Deadline(cfg.RequestTimeout),
		Middleware(auth.Middleware(d.Issuer)),
	)
}
