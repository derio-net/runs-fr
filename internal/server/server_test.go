package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/derio-net/runs-fr/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Addr:          ":8080",
		Namespace:     "agents",
		LabelSelector: "fr.run/id",
		AuthHeader:    "X-Forwarded-User",
	}
}

func TestHealthz(t *testing.T) {
	h := New(testConfig())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Result().Body)
	if got := string(body); got != "ok" {
		t.Errorf("GET /healthz body = %q, want %q", got, "ok")
	}
}

func TestReadyz(t *testing.T) {
	h := New(testConfig())

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Phase 1: readiness is a static 200; Phase 2 enhances it to probe API
	// reachability.
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /readyz status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Result().Body)
	if got := string(body); got != "ok" {
		t.Errorf("GET /readyz body = %q, want %q", got, "ok")
	}
}

// TestHealthzNotGated guards that liveness works pre-auth: kubelet probes carry
// no forward-auth header, so /healthz must answer 200 without one.
func TestHealthzNotGated(t *testing.T) {
	h := New(testConfig())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /healthz (no auth header) status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestAPIGated guards that /api/* is behind the forward-auth middleware: a
// request without the trusted header is rejected with 403 before reaching any
// handler.
func TestAPIGated(t *testing.T) {
	h := New(testConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/runs (no auth header) status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
