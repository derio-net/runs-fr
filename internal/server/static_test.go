package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// staticFixture is a stand-in for the embedded frontend bundle: the real assets
// are embedded in package main (cmd/runs-fr) and injected via WithStatic, so
// the server's serving logic is tested against an in-memory FS instead.
func staticFixture() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {Data: []byte(`<!DOCTYPE html><html><body><div id="terminal"></div><script src="/bundle.js"></script></body></html>`)},
		"bundle.js":  {Data: []byte(`console.log("hi");`)},
		"xterm.css":  {Data: []byte(`.xterm{}`)},
	}
}

// TestStaticIndex is the core contract: GET / serves the embedded page shell so
// the browser has a mount node to boot the terminal into.
func TestStaticIndex(t *testing.T) {
	h := New(testConfig(), WithStatic(staticFixture()))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(rec.Result().Body)
	if !strings.Contains(string(body), `id="terminal"`) {
		t.Errorf("GET / body missing terminal mount node, got %q", body)
	}
}

// TestStaticSecurityHeaders guards the baseline hardening on the page response:
// no MIME sniffing and a restrictive CSP.
func TestStaticSecurityHeaders(t *testing.T) {
	h := New(testConfig(), WithStatic(staticFixture()))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp == "" {
		t.Error("Content-Security-Policy header missing")
	}
}

// TestStaticAssets serves the bundled script and stylesheet with sensible
// content types so the browser executes/parses them correctly.
func TestStaticAssets(t *testing.T) {
	h := New(testConfig(), WithStatic(staticFixture()))

	cases := []struct {
		path        string
		wantCTPart  string
		wantBodyHas string
	}{
		{"/bundle.js", "javascript", `console.log`},
		{"/xterm.css", "css", `.xterm`},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want %d", tc.path, rec.Code, http.StatusOK)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, tc.wantCTPart) {
			t.Errorf("GET %s Content-Type = %q, want containing %q", tc.path, ct, tc.wantCTPart)
		}
		body, _ := io.ReadAll(rec.Result().Body)
		if !strings.Contains(string(body), tc.wantBodyHas) {
			t.Errorf("GET %s body = %q, want containing %q", tc.path, body, tc.wantBodyHas)
		}
	}
}

// TestStaticUnknownPath guards that the static handler does not become a
// catch-all: an unmapped path returns 404, not the index page.
func TestStaticUnknownPath(t *testing.T) {
	h := New(testConfig(), WithStatic(staticFixture()))

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /does-not-exist.js status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestStaticDisabled guards that without WithStatic the server registers no root
// route, so the frontend is purely opt-in (the Phase 1 behaviour is preserved).
func TestStaticDisabled(t *testing.T) {
	h := New(testConfig())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET / (no static) status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
