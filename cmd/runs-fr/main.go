// Command runs-fr is the portable web shell gateway for agentic runs: it
// lists live runs and bridges a run's named session to an xterm.js terminal
// in the browser — via pods/exec (kube backend) or a local tmux server
// (local backend).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/derio-net/runs-fr/internal/config"
	"github.com/derio-net/runs-fr/internal/kube"
	"github.com/derio-net/runs-fr/internal/localterm"
	"github.com/derio-net/runs-fr/internal/runs"
	"github.com/derio-net/runs-fr/internal/server"
	"github.com/derio-net/runs-fr/internal/term"
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

	// Per-backend seams: the server is backend-agnostic behind these three.
	var (
		lister    server.RunLister
		resolver  server.RunResolver
		attacher  server.Attacher
		readiness server.ReadyFunc
	)
	switch cfg.Backend {
	case "local":
		// Local backend: runs are tmux sessions on this host. Fail fast when
		// tmux is absent (an image problem); a missing tmux SERVER is just
		// zero sessions, so readiness re-checks only the binary.
		if _, err := exec.LookPath("tmux"); err != nil {
			log.Error("local backend requires tmux on PATH", "err", err)
			os.Exit(1)
		}
		l := localterm.NewLister(cfg.TmuxSocket, cfg.RunnerName)
		lister, resolver = l, l
		attacher = localterm.NewAttacher(cfg.TmuxSocket)
		readiness = func(context.Context) error {
			_, err := exec.LookPath("tmux")
			return err
		}
	default: // "kube" — config.Load rejects anything else
		// Build the Kubernetes client surface up front: the gateway can't
		// function without it, so fail fast rather than degrade.
		selector, err := kube.BuildSelector(cfg)
		if err != nil {
			log.Error("build selector", "err", err)
			os.Exit(1)
		}
		restCfg, err := kube.RestConfig(cfg)
		if err != nil {
			log.Error("resolve kubernetes config", "err", err)
			os.Exit(1)
		}
		clientset, err := kube.NewClientset(cfg)
		if err != nil {
			log.Error("build kubernetes client", "err", err)
			os.Exit(1)
		}
		l := runs.NewLister(clientset, cfg.Namespace, selector)
		lister, resolver = l, l
		attacher = term.NewKubeAttacher(restCfg, clientset, cfg.Namespace)
		readiness = kube.Readiness(clientset, cfg.Namespace)
	}

	// Trip cancellation on SIGTERM/SIGINT so rollouts drain in-flight attaches
	// instead of dropping them.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: server.New(cfg,
			server.WithReadiness(readiness),
			server.WithLister(lister),
			// The resolver maps a run id to its attach handle (pod name or
			// tmux session); the attacher bridges the stream. Bind sessions
			// to ctx so a SIGTERM drains them.
			server.WithAttach(resolver, attacher),
			server.WithShutdownContext(ctx),
			// Serve the embedded xterm.js frontend at /.
			server.WithStatic(staticFS()),
		),
	}

	// Run the listener in the background so main can wait on the signal context.
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "backend", cfg.Backend, "namespace", cfg.Namespace)
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
