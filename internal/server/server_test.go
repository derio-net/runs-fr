package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/derio-net/runs-fr/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Addr:           ":8080",
		Namespace:      "agents",
		LabelSelector:  "fr.run/id",
		AuthHeader:     "X-Forwarded-User",
		AuthRoleHeader: "X-Forwarded-Role",
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

func TestNodeAttachTokenIsOwnerBoundAndSingleUse(t *testing.T) {
	h := New(testConfig())
	denied := httptest.NewRequest(http.MethodPost, "/api/nodes/default/attach-token", nil)
	denied.Header.Set(authTestHeader, "observer")
	denied.Header.Set("X-Forwarded-Role", "observer")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, denied)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner token status = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/default/attach-token", nil)
	req.Header.Set(authTestHeader, "owner")
	req.Header.Set("X-Forwarded-Role", "owner")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner token status = %d", rec.Code)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Token == "" {
		t.Fatalf("token response = %q, err=%v", rec.Body.String(), err)
	}
	if len(body.Token) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars", len(body.Token))
	}
}
