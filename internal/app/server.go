// Package app holds the use cases behind the HTTP operations.
//
// Go for Spring people: Server is the one big "controller + service" object. Embedding Unimplemented
// (a struct inside a struct, no field name) copies its methods onto Server, like inheriting from an abstract
// adapter class. Methods Server defines itself (Health, Ready) win over the embedded ones, like @Override.
package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/config"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/feedmedia"
	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// Version is reported by GET /health. Overridden at build time with -ldflags "-X .../app.Version=1.2.3".
var Version = "dev"

// Server implements api.StrictServerInterface. Other agents add operations by defining methods on it
// (in new files) and the matching 501 stub from Unimplemented is replaced automatically.
type Server struct {
	Unimplemented // 501 for every operation not overridden below

	Store   *store.Store
	Config  config.Config
	Log     *slog.Logger
	Issuer  *auth.Issuer
	Refresh *auth.RefreshTokens
	// PINLimit: 5 wrong PINs per child per 15 minutes (contract 2.5). Use Locked/Fail/Reset in createChildToken.
	PINLimit *auth.AttemptLimiter
	// LoginLimit: 5 failures per email per 15 minutes (contract section 16).
	LoginLimit *auth.AttemptLimiter
	// WebAuthn runs passkey ceremonies (nil = passkeys off, endpoints answer 503). See passkeys.go.
	WebAuthn *webauthn.WebAuthn
	// Payments is the payment processor (nil = dev provider outside production, none in production). See platform_billing.go.
	Payments PaymentProvider
	// Sync records outbox rows for the optional MongoDB copy (nil or disabled = no-op). See mongo_sync.go.
	Sync *mongosync.Outbox
	// Content is the curated question bank repository (cache included); main shares it with the admin API and
	// sets Content.Out to the same outbox as Sync. See content_play.go.
	Content *contentdb.Repo
	// Assets is the image store behind world art, question media and the /assets endpoint.
	Assets *assetstore.Store
	// Feed is the curated Discover item repository (cache included). See feed_discover.go.
	Feed *feedmedia.Repo
}

// NewServer wires the dependencies (Spring: constructor injection; no container, main() calls this).
func NewServer(s *store.Store, cfg config.Config, log *slog.Logger) *Server {
	return &Server{
		Store:      s,
		Config:     cfg,
		Log:        log,
		Issuer:     auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.ChildTTL),
		Refresh:    auth.NewRefreshTokens(s, cfg.RefreshTTL),
		PINLimit:   auth.NewAttemptLimiter(5, 15*time.Minute),
		LoginLimit: auth.NewAttemptLimiter(5, 15*time.Minute),
		WebAuthn:   newWebAuthn(cfg),
		Content:    contentdb.New(s, nil),
		Assets:     assetstore.New(s),
		Feed:       feedmedia.NewRepo(s, time.Minute),
	}
}

// Health answers GET /health: process is alive, no database call (contract: cheap liveness).
func (s *Server) Health(ctx context.Context, _ api.HealthRequestObject) (api.HealthResponseObject, error) {
	narrator := api.Health200JSONResponseBodyNarrator("stub") // no LLM yet
	version := Version
	return api.Health200JSONResponse{Status: "ok", Narrator: &narrator, Version: &version}, nil
}

// Ready answers GET /ready: 200 only if the database answers within 2 seconds, else 503.
func (s *Server) Ready(ctx context.Context, _ api.ReadyRequestObject) (api.ReadyResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		s.Log.Error("readiness check failed", "err", err)
		status := api.Ready503JSONResponseBodyStatus("unavailable")
		checks := map[string]api.Ready503JSONResponseBodyChecks{"db": "down"}
		return api.Ready503JSONResponse{Status: &status, Checks: &checks}, nil
	}
	return api.Ready200JSONResponse{
		Status: "ok",
		Checks: map[string]api.Ready200JSONResponseBodyChecks{"db": "ok"},
	}, nil
}
