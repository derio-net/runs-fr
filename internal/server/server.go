// Package server wires the gateway's HTTP surface: health/readiness probes
// today, and the run-list + attach endpoints in later phases.
package server

import (
	"log/slog"
	"net/http"

	"github.com/derio-net/runs-fr/internal/config"
)

// server holds the dependencies shared across handlers.
type server struct {
	cfg *config.Config
	log *slog.Logger
}

// New builds the gateway's HTTP handler from the given configuration. The
// returned handler serves liveness (/healthz) and readiness (/readyz) probes;
// the run-list and attach routes are added in later phases.
func New(cfg *config.Config) http.Handler {
	s := &server{
		cfg: cfg,
		log: slog.Default(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	return mux
}

// handleHealthz is the liveness probe: it reports that the process is up.
func (s *server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleReadyz is the readiness probe. In Phase 1 it is a static 200; Phase 2
// enhances it to verify Kubernetes API reachability before reporting ready.
func (s *server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
