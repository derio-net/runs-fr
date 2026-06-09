package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/derio-net/runs-fr/internal/runs"
)

// stubResolver resolves an id to a fixed pod name, or reports not-found.
type stubResolver struct {
	pod      string
	notFound bool
	gotID    string
}

func (s *stubResolver) Resolve(_ context.Context, id string) (string, error) {
	s.gotID = id
	if s.notFound {
		return "", runs.ErrNotFound
	}
	return s.pod, nil
}

// attachCall captures the arguments the handler passes to the attacher.
type attachCall struct {
	pod     string
	session string
}

// stubAttacher records its invocation and runs an injected behaviour, standing
// in for the real exec runner (which needs a kubelet — Phase 8).
type stubAttacher struct {
	calls    chan attachCall
	behavior func(ctx context.Context, conn *websocket.Conn) error
}

func (s *stubAttacher) Attach(ctx context.Context, conn *websocket.Conn, pod, session string) error {
	s.calls <- attachCall{pod: pod, session: session}
	if s.behavior != nil {
		return s.behavior(ctx, conn)
	}
	return nil
}

func newStubAttacher() *stubAttacher {
	return &stubAttacher{calls: make(chan attachCall, 1)}
}

// (a) The attach route lives under /api/, so a request without the trusted
// forward-auth header is rejected with 403 before any upgrade.
func TestAttach_NoAuthHeader403(t *testing.T) {
	h := New(testConfig(), WithAttach(&stubResolver{pod: "p"}, newStubAttacher()))

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1/attach", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// (b) An id that fails ^[a-z0-9-]+$ is rejected with 400 before any upgrade.
func TestAttach_InvalidID400(t *testing.T) {
	resolver := &stubResolver{pod: "p"}
	h := New(testConfig(), WithAttach(resolver, newStubAttacher()))

	req := httptest.NewRequest(http.MethodGet, "/api/runs/BAD_id/attach", nil)
	req.Header.Set(authTestHeader, "tester")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if resolver.gotID != "" {
		t.Errorf("resolver called for an invalid id (%q); validation must come first", resolver.gotID)
	}
}

// (b) A valid id that matches no run pod is rejected with 404 before any upgrade.
func TestAttach_NoMatchingPod404(t *testing.T) {
	h := New(testConfig(), WithAttach(&stubResolver{notFound: true}, newStubAttacher()))

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1/attach", nil)
	req.Header.Set(authTestHeader, "tester")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// dialWS opens a websocket against a test server, carrying the trusted header.
func dialWS(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + path
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{authTestHeader: []string{"tester"}},
	})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	return conn
}

// (c) A valid, matched id with the header completes the websocket handshake and
// hands the resolved pod (not the raw path segment) to the attacher, whose
// frames reach the browser.
func TestAttach_HandshakeAndPipe(t *testing.T) {
	resolver := &stubResolver{pod: "pod-real-name"}
	attacher := newStubAttacher()
	attacher.behavior = func(ctx context.Context, conn *websocket.Conn) error {
		// Prove the live conn was handed over by emitting a terminal frame.
		return conn.Write(ctx, websocket.MessageBinary, []byte("hello-terminal"))
	}

	srv := httptest.NewServer(New(testConfig(), WithAttach(resolver, attacher)))
	defer srv.Close()

	conn := dialWS(t, srv, "/api/runs/run-1/attach")
	defer conn.Close(websocket.StatusNormalClosure, "")

	typ, data, err := conn.Read(context.Background())
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if typ != websocket.MessageBinary || string(data) != "hello-terminal" {
		t.Errorf("frame = (%v, %q), want (binary, %q)", typ, data, "hello-terminal")
	}

	select {
	case call := <-attacher.calls:
		if call.pod != "pod-real-name" {
			t.Errorf("attacher pod = %q, want resolved %q", call.pod, "pod-real-name")
		}
		if call.session == "" {
			t.Error("attacher session name is empty; must be derived from the id")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("attacher was never called")
	}
	if resolver.gotID != "run-1" {
		t.Errorf("resolver id = %q, want %q", resolver.gotID, "run-1")
	}
}

// (d) Cancelling the session context (here via server shutdown) makes the
// attach goroutine return — no leak.
func TestAttach_ContextCancelStopsAttacher(t *testing.T) {
	shutdown, cancel := context.WithCancel(context.Background())
	defer cancel()

	exited := make(chan struct{})
	attacher := newStubAttacher()
	attacher.behavior = func(ctx context.Context, conn *websocket.Conn) error {
		<-ctx.Done() // block until the derived context is cancelled
		close(exited)
		return ctx.Err()
	}

	srv := httptest.NewServer(New(testConfig(),
		WithAttach(&stubResolver{pod: "pod-x"}, attacher),
		WithShutdownContext(shutdown),
	))
	defer srv.Close()

	conn := dialWS(t, srv, "/api/runs/run-1/attach")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Wait until the attacher is running before cancelling.
	select {
	case <-attacher.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("attacher was never called")
	}

	cancel() // server shutdown signal

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("attacher goroutine did not exit on context cancel (leak)")
	}
}
