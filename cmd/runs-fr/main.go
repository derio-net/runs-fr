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
	"github.com/derio-net/runs-fr/internal/kube"
	"github.com/derio-net/runs-fr/internal/runs"
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

	// Build the Kubernetes client surface up front: the gateway can't function
	// without it, so fail fast rather than degrade.
	selector, err := kube.BuildSelector(cfg)
	if err != nil {
		log.Error("build selector", "err", err)
		os.Exit(1)
	}
	clientset, err := kube.NewClientset(cfg)
	if err != nil {
		log.Error("build kubernetes client", "err", err)
		os.Exit(1)
	}

	lister := runs.NewLister(clientset, cfg.Namespace, selector)
	readiness := kube.Readiness(clientset, cfg.Namespace)

	// Trip cancellation on SIGTERM/SIGINT so rollouts drain in-flight attaches
	// instead of dropping them.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: server.New(cfg, server.WithReadiness(readiness), server.WithLister(lister)),
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
		// Reached when ListenAndServe fails before any signal — e.g. the
		// listen address is already in use. (After a signal, ListenAndServe
		// returns ErrServerClosed, which the goroutine maps to a nil send that
		// is simply drained as the process exits via the ctx.Done branch.)
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
