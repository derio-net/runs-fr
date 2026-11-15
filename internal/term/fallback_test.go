package term

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// TestNewExecutorFallsBackToSPDYOnUpgradeFailure drives the real client-go
// transports against an API server stand-in that refuses the WebSocket upgrade
// (an older API server, or a proxy that strips Upgrade). The executor must then
// retry over SPDY (POST). The predicate passed to NewFallbackExecutor has to
// recognise the error type client-go's WebSocket transport actually returns —
// from k8s.io/client-go v0.36 that is k8s.io/streaming's UpgradeFailureError,
// which apimachinery's deprecated httpstream.IsUpgradeFailure does not match.
func TestNewExecutorFallsBackToSPDYOnUpgradeFailure(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		http.Error(w, "streaming upgrades are not supported here", http.StatusBadRequest)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL + "/api/v1/namespaces/ns/pods/p/exec?command=true")
	if err != nil {
		t.Fatal(err)
	}
	exec, err := NewExecutor(&rest.Config{Host: srv.URL}, u)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: io.Discard, Tty: true}); err == nil {
		t.Fatal("stream against a server that refuses every upgrade returned nil")
	}

	mu.Lock()
	defer mu.Unlock()
	var sawGET, sawPOST bool
	for _, m := range methods {
		sawGET = sawGET || m == http.MethodGet
		sawPOST = sawPOST || m == http.MethodPost
	}
	if !sawGET {
		t.Errorf("WebSocket attempt (GET) never reached the server; methods=%v", methods)
	}
	if !sawPOST {
		t.Errorf("no SPDY fallback (POST) after the WebSocket upgrade failed; methods=%v", methods)
	}
}
