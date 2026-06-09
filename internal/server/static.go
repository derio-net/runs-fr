package server

import (
	"io/fs"
	"net/http"
)

// contentSecurityPolicy is the baseline CSP for the frontend. It is deliberately
// tight: default-src 'none' denies everything, then only the origins the app
// actually needs are re-enabled. script-src 'self' admits the embedded
// bundle.js (and forbids inline script); style-src allows 'unsafe-inline'
// because xterm.js sets inline styles on the terminal DOM; connect-src 'self'
// covers the same-origin attach websocket (ws/wss to 'self' is permitted).
const contentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'"

// staticAsset names a file in the embedded FS and the content type it is served
// with. Content types are pinned rather than sniffed so X-Content-Type-Options:
// nosniff never has to guess.
type staticAsset struct {
	file        string
	contentType string
}

// staticAssets maps request paths to the embedded files that back them. The
// page shell is served at the site root; the bundle and stylesheet at fixed
// paths the shell references.
var staticAssets = map[string]staticAsset{
	"/bundle.js": {file: "bundle.js", contentType: "text/javascript; charset=utf-8"},
	"/xterm.css": {file: "xterm.css", contentType: "text/css; charset=utf-8"},
}

// WithStatic serves the embedded frontend (page shell, bundle, stylesheet) from
// fsys. Without it the gateway serves no UI and GET / is a 404 — the frontend is
// opt-in so headless/API-only deployments stay minimal. fsys is expected to be
// rooted at the static directory (so files are "index.html", "bundle.js", …).
func WithStatic(fsys fs.FS) Option {
	return func(s *server) { s.static = fsys }
}

// registerStatic wires the frontend routes onto mux when a static FS is present.
// Routes are ungated (no forward-auth): the assets carry no run data, and the
// upstream proxy already gates the whole origin. Only the API needs the in-app
// gate. GET /{$} matches the root path exactly so this never becomes a
// catch-all that shadows unmapped paths.
func (s *server) registerStatic(mux *http.ServeMux) {
	if s.static == nil {
		return
	}
	mux.HandleFunc("GET /{$}", s.serveAsset(staticAsset{file: "index.html", contentType: "text/html; charset=utf-8"}))
	for path, asset := range staticAssets {
		mux.HandleFunc("GET "+path, s.serveAsset(asset))
	}
}

// serveAsset returns a handler that writes a single embedded file with the
// baseline security headers. A missing file is a 404 rather than a 500: in a
// build without `make frontend`, bundle.js may be absent, and a clear Not Found
// beats a server error.
func (s *server) serveAsset(asset staticAsset) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(s.static, asset.file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", asset.contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		_, _ = w.Write(data)
	}
}
