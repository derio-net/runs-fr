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
	// Kubeconfig is an optional path to a kubeconfig file. When empty the
	// in-cluster config is used.
	Kubeconfig string
}

// Load reads the configuration from the environment, applying defaults. It
// returns an error if a required value (NAMESPACE) is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:          envOr("ADDR", ":8080"),
		Namespace:     os.Getenv("NAMESPACE"),
		LabelSelector: envOr("LABEL_SELECTOR", "fr.run/id"),
		AuthHeader:    envOr("AUTH_HEADER", "X-Forwarded-User"),
		Kubeconfig:    os.Getenv("KUBECONFIG"),
	}

	if cfg.Namespace == "" {
		return nil, errors.New("config: NAMESPACE is required")
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
