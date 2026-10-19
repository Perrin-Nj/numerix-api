// Command api starts the Numerix HTTP server.
//
// Go for Spring people: package main with a func main() is the program entry point
// (Spring: the class with @SpringBootApplication). This file is the "composition root":
// the one place that builds objects and wires them together.
package main

import (
	"context"
	"errors"
	"log/slog" // structured logging in the standard library (Spring: SLF4J + Logback)
	"net/http"
	"net/http/pprof" // profiling handlers; mounted only when PPROF=true (config refuses it in production)
	"os"
	"os/signal" // lets us catch Ctrl+C / SIGTERM for a clean shutdown
	"syscall"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/adminapi"
	"github.com/Perrin-Nj/numerix-api/internal/app"
	"github.com/Perrin-Nj/numerix-api/internal/config"
	"github.com/Perrin-Nj/numerix-api/internal/drafter"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

func main() {
	if err := run(); err != nil { // run returns errors; main alone decides the exit code
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// run does the real work so deferred cleanups (store.Close, stop) execute before os.Exit.
func run() error {
	cfg, err := config.Load(os.Getenv) // fails fast on bad settings (e.g. no JWT_SECRET in production)
	if err != nil {
		return err
	}
	// JSON logs in production (easy for hosts to collect), readable text locally.
	var h slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if cfg.IsProduction() {
		h = slog.NewJSONHandler(os.Stdout, nil)
	}
	log := slog.New(h)

	// ctx is cancelled when the process receives Ctrl+C or a stop signal from the host.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop() // defer = run when run() returns (Spring: try/finally)

	db, err := store.Open(ctx, cfg.DBPath) // opens SQLite and applies migrations
	if err != nil {
		return err
	}
	defer db.Close()

	server := app.NewServer(db, cfg, log)

	// Optional MongoDB copy (personal bests, family board, anonymised stats). Off unless MONGODB_URI is set.
	var syncStats func() mongosync.Stats
	if cfg.MongoURI == "" {
		log.Info("mongo sync is off (MONGODB_URI not set); SQLite serves everything")
		syncStats = func() mongosync.Stats { return mongosync.Stats{} }
	} else {
		sink, err := mongosync.NewMongo(ctx, cfg.MongoURI, cfg.MongoDB)
		if err != nil {
			return err
		}
		server.Sync = mongosync.NewOutbox(true)
		w := mongosync.NewWorker(db, sink, server.SyncResolver(mongosync.NewPseudonymizer(cfg.MongoPseudoSecret)), log, true)
		syncStats = w.Stats
		wctx, wcancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { // the worker goroutine; closing done tells shutdown that the last flush is finished
			defer close(done)
			w.Run(wctx)
		}()
		defer func() { // runs before db.Close (defers run last-in first-out): stop the worker, then disconnect
			wcancel()
			<-done
			cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := sink.Close(cctx); err != nil {
				log.Warn("mongo close failed", "err", err)
			}
		}()
		log.Info("mongo sync is on", "db", cfg.MongoDB)
	}
	server.Content.Out = server.Sync // content changes queue Mongo events in the same transactions (nil when Mongo is off)
	// Root router: /admin/v1/* goes to the admin API (its own auth + CORS), everything else to the public API.
	root := http.NewServeMux()
	root.Handle("/admin/v1/", adminapi.New(adminapi.Deps{
		Store: db, Log: log, Token: cfg.AdminAPIToken, Production: cfg.IsProduction(), Origins: os.Getenv("ADMIN_ORIGIN"), Sync: syncStats,
		Content: server.Content, Assets: server.Assets, Drafter: drafter.FromEnv(os.Getenv, log),
	}))
	if cfg.PprofEnabled { // dev-only profiler: go tool pprof http://localhost:PORT/debug/pprof/profile
		root.HandleFunc("/debug/pprof/", pprof.Index)
		root.HandleFunc("/debug/pprof/profile", pprof.Profile)
		root.HandleFunc("/debug/pprof/trace", pprof.Trace)
		root.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		root.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	}
	root.Handle("/", httpapi.NewHandler(httpapi.Deps{
		Impl: server, Config: cfg, Log: log, Issuer: server.Issuer,
	}))
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: root,
		// Timeouts protect us from slow or stuck clients when many kids connect at once.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Start serving in a goroutine (a lightweight thread); report a failure through a channel.
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "env", cfg.Env)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done(): // signal received
		log.Info("shutting down")
	case err := <-errCh: // server stopped by itself
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	// Give in-flight requests up to 10 seconds to finish (graceful shutdown).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
