// Command runs-fr is the Kubernetes-native web shell gateway: it lists live
// agent-run pods and bridges a pod's named session to an xterm.js terminal in
// the browser.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/derio-net/runs-fr/internal/config"
	"github.com/derio-net/runs-fr/internal/server"
)

// shutdownTimeout bounds how long graceful shutdown waits for in-flight
// attaches to drain before forcing connections closed.
const shutdownTimeout = 30 * time.Second

func main() {
	log := slog.Default()

	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "err", err)
		os.Exit(1)
	}

	// Trip cancellation on SIGTERM/SIGINT so rollouts drain in-flight attaches
	// instead of dropping them.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: server.New(cfg),
	}

	// Run the listener in the background so main can wait on the signal context.
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "namespace", cfg.Namespace)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			log.Error("server error", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		log.Info("shutdown signal received, draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("graceful shutdown", "err", err)
			os.Exit(1)
		}
		log.Info("shutdown complete")
	}
}
