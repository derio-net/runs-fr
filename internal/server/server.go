// Package server wires the gateway's HTTP surface: health/readiness probes
// today, and the run-list + attach endpoints in later phases.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/derio-net/runs-fr/internal/config"
	"github.com/derio-net/runs-fr/internal/runs"
)

// ReadyFunc reports whether the gateway's dependencies are reachable. A non-nil
// error makes /readyz return 503 so the pod is pulled from Service endpoints.
type ReadyFunc func(context.Context) error

// RunLister lists the live runs served at /api/runs.
type RunLister interface {
	List(context.Context) ([]runs.Run, error)
}

// server holds the dependencies shared across handlers. log is also carried by
// the attach handler added in a later phase.
type server struct {
	cfg    *config.Config
	log    *slog.Logger
	ready  ReadyFunc
	lister RunLister
}

// Option configures the server's injected dependencies.
type Option func(*server)

// WithReadiness sets the readiness check backing /readyz. Without it, /readyz
// reports ready unconditionally (the Phase 1 behaviour).
func WithReadiness(fn ReadyFunc) Option {
	return func(s *server) { s.ready = fn }
}

// WithLister sets the runs lister backing GET /api/runs. The route is only
// registered when a lister is provided.
func WithLister(l RunLister) Option {
	return func(s *server) { s.lister = l }
}

// New builds the gateway's HTTP handler from the given configuration. The
// returned handler serves liveness (/healthz) and readiness (/readyz) probes;
// the run-list and attach routes are added in later phases.
func New(cfg *config.Config, opts ...Option) http.Handler {
	s := &server{
		cfg:   cfg,
		log:   slog.Default(),
		ready: func(context.Context) error { return nil },
	}
	for _, opt := range opts {
		opt(s)
	}

	mux := http.NewServeMux()

	// Liveness/readiness probes are ungated: kubelet carries no forward-auth
	// header, so gating them would break the probes.
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)

	// Everything under /api/ (the run-list now, the attach route in a later
	// phase) is gated by the trusted forward-auth header: apiMux is mounted
	// behind the middleware so an unauthenticated request is rejected with 403
	// before reaching any handler. The sub-mux sees the original request path
	// (ServeMux does not strip the prefix), so routes register their full path.
	apiMux := http.NewServeMux()
	if s.lister != nil {
		apiMux.HandleFunc("GET /api/runs", s.handleRuns)
	}
	mux.Handle("/api/", RequireForwardAuth(s.cfg.AuthHeader)(apiMux))

	return mux
}

// handleHealthz is the liveness probe: it reports that the process is up.
func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s.log.Debug("liveness probe", "remote", r.RemoteAddr)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleReadyz is the readiness probe: it runs the injected readiness check
// (Kubernetes API/RBAC reachability) and reports 503 if it fails, so a pod with
// a broken ServiceAccount is removed from Service endpoints.
func (s *server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.ready(r.Context()); err != nil {
		s.log.Warn("readiness check failed", "remote", r.RemoteAddr, "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready"))
		return
	}
	s.log.Debug("readiness probe", "remote", r.RemoteAddr)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleRuns serves the live runs as a JSON array.
func (s *server) handleRuns(w http.ResponseWriter, r *http.Request) {
	rs, err := s.lister.List(r.Context())
	if err != nil {
		s.log.Error("list runs", "err", err)
		http.Error(w, "failed to list runs", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rs); err != nil {
		// The status line is already committed; just record the write failure.
		s.log.Error("encode runs", "err", err)
	}
}
