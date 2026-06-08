package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestReadyzUnready proves readiness reflects API/RBAC reachability: when the
// injected checker fails, the pod reports 503 so it is pulled from Service
// endpoints.
func TestReadyzUnready(t *testing.T) {
	h := New(testConfig(), WithReadiness(func(context.Context) error {
		return errors.New("pods.list forbidden")
	}))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestReadyzReady(t *testing.T) {
	h := New(testConfig(), WithReadiness(func(context.Context) error { return nil }))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /readyz status = %d, want %d", rec.Code, http.StatusOK)
	}
}
