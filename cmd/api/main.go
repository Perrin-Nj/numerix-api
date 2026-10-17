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
	"os"
	"os/signal" // lets us catch Ctrl+C / SIGTERM for a clean shutdown
	"syscall"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/app"
	"github.com/Perrin-Nj/numerix-api/internal/config"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
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
	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: httpapi.NewHandler(httpapi.Deps{
			Impl: server, Config: cfg, Log: log, Issuer: server.Issuer,
		}),
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
