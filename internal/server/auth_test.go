package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler records whether it was invoked and writes 200 when it is. It stands
// in for the protected handler the middleware guards.
type okHandler struct{ called bool }

func (h *okHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusOK)
}

const authTestHeader = "X-Forwarded-User"

func TestRequireForwardAuth_PresentCallsNext(t *testing.T) {
	next := &okHandler{}
	h := RequireForwardAuth(authTestHeader)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	req.Header.Set(authTestHeader, "alice")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !next.called {
		t.Error("next handler was not called when auth header present")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireForwardAuth_AbsentReturns403(t *testing.T) {
	next := &okHandler{}
	h := RequireForwardAuth(authTestHeader)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if next.called {
		t.Error("next handler was called when auth header absent")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireForwardAuth_EmptyReturns403(t *testing.T) {
	next := &okHandler{}
	h := RequireForwardAuth(authTestHeader)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	req.Header.Set(authTestHeader, "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if next.called {
		t.Error("next handler was called when auth header empty")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
