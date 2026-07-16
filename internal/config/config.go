// Package config loads the runs-fr gateway configuration from the environment.
//
// The gateway is deliberately cluster-agnostic: the namespace it watches, the
// label selector that identifies run pods, and the trusted auth header are all
// configuration, so the same image installs into any cluster.
package config

import (
	"errors"
	"os"
)

// Config holds the runtime configuration for the gateway.
type Config struct {
	// Addr is the listen address (default ":8080").
	Addr string
	// Namespace is the Kubernetes namespace to watch for run pods. Required.
	Namespace string
	// LabelSelector selects run pods (default "fr.run/id").
	LabelSelector string
	// AuthHeader is the trusted header carrying the authenticated user
	// (default "X-Forwarded-User"), set by an upstream forward-auth proxy.
	AuthHeader string
	// AuthRoleHeader is the trusted header carrying the authenticated user's
	// role (default "X-Forwarded-Role"), used to gate owner-only surfaces
	// (node shells). Like AuthHeader it is untrusted UNLESS the upstream
	// forward-auth proxy overwrites it on every request — a passthrough proxy
	// that forwards a client-supplied value lets any caller assert "owner", so
	// the proxy MUST set/strip it. Configurable so the same image installs
	// behind any proxy's chosen header.
	AuthRoleHeader string
	// Kubeconfig is an optional path to a kubeconfig file. When empty the
	// in-cluster config is used.
	Kubeconfig string
	// Backend selects the attach backend: "kube" (default) bridges the
	// pods/exec subresource; "local" bridges tmux sessions on the gateway's
	// own host (the CNC laptop-runner shape — gateway and sessions share a
	// container, so no Docker socket or cluster is involved).
	Backend string
	// TmuxSocket optionally names an explicit tmux socket path (tmux -S).
	// Local backend only.
	TmuxSocket string
	// RunnerName optionally labels this gateway's runs (surfaced as the
	// Run.Runner field) so aggregated multi-runner lists stay
	// distinguishable. Local backend only.
	RunnerName string
}

// Load reads the configuration from the environment, applying defaults. It
// returns an error if a required value (NAMESPACE) is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:           envOr("ADDR", ":8080"),
		Namespace:      os.Getenv("NAMESPACE"),
		LabelSelector:  envOr("LABEL_SELECTOR", "fr.run/id"),
		AuthHeader:     envOr("AUTH_HEADER", "X-Forwarded-User"),
		AuthRoleHeader: envOr("AUTH_ROLE_HEADER", "X-Forwarded-Role"),
		Kubeconfig:     os.Getenv("KUBECONFIG"),
		Backend:        envOr("BACKEND", "kube"),
		TmuxSocket:     os.Getenv("TMUX_SOCKET"),
		RunnerName:     os.Getenv("RUNNER_NAME"),
	}

	switch cfg.Backend {
	case "kube":
		// The namespace scopes pods/exec; only the kube backend needs it.
		if cfg.Namespace == "" {
			return nil, errors.New("config: NAMESPACE is required")
		}
	case "local":
	default:
		return nil, errors.New(`config: BACKEND must be "kube" or "local"`)
	}

	return cfg, nil
}

// envOr returns the value of the environment variable key, or def if it is
// unset or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
