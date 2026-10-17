// Command api starts the Numerix HTTP server.
//
// Go for Spring people: package main with a func main() is the program entry point
// (Spring: the class with @SpringBootApplication and public static void main).
// This file is the "composition root": the one place that builds objects and wires them together.
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

	"github.com/Perrin-Nj/numerix-api/internal/httpapi" // our own package, imported by module path
)

func main() {
	// Logger that prints JSON lines to standard output (easy for hosts to collect).
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Read the port from the environment (Spring: server.port in application.yml). Default 8080.
	addr := ":" + envOr("PORT", "8080")

	// http.Server holds the settings. The timeouts protect us from slow or stuck clients,
	// which matters when many kids connect at once.
	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ctx is cancelled when the process receives Ctrl+C or a stop signal from the host.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop() // defer = run this when main returns (Spring: try/finally)

	// Start the server in a goroutine, a lightweight thread. "go f()" runs f concurrently.
	errCh := make(chan error, 1) // a channel is a typed pipe between goroutines (like a one-slot queue)
	go func() {
		log.Info("listening", "addr", addr)
		errCh <- srv.ListenAndServe() // blocks until the server stops, then sends the error into the channel
	}()

	// Wait for either a stop signal or a server failure.
	select {
	case <-ctx.Done(): // signal received
		log.Info("shutting down")
	case err := <-errCh: // server stopped by itself
		if !errors.Is(err, http.ErrServerClosed) { // ErrServerClosed is the normal result of Shutdown
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}

	// Give in-flight requests up to 10 seconds to finish (graceful shutdown).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown failed", "err", err)
	}
}

// envOr returns the environment variable key, or fallback when it is empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
