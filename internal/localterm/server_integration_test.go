package localterm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/derio-net/runs-fr/internal/config"
	"github.com/derio-net/runs-fr/internal/server"
)

// The local backend must plug into the server's seams exactly like the kube
// one — same routes, same forward-auth gate.
var (
	_ server.RunLister   = (*Lister)(nil)
	_ server.RunResolver = (*Lister)(nil)
	_ server.Attacher    = (*Attacher)(nil)
)

func TestLocalBackendServesGatedRunList(t *testing.T) {
	l := NewLister("", "default")
	l.runTmux = func(context.Context, ...string) ([]byte, error) {
		return []byte("run-1\n"), nil
	}

	h := server.New(
		&config.Config{AuthHeader: "X-Forwarded-User"},
		server.WithLister(l),
		server.WithAttach(l, NewAttacher("")),
	)

	ungated := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ungated)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no-header GET /api/runs = %d, want 403", rec.Code)
	}

	gated := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	gated.Header.Set("X-Forwarded-User", "you")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, gated)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed GET /api/runs = %d, want 200 (body=%s)", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"id":"run-1"`) || !strings.Contains(body, `"runner":"default"`) {
		t.Errorf("run list = %s, want run-1 tagged with runner default", body)
	}
}
