package main

import (
	"embed"
	"io/fs"
)

// staticFiles holds the built frontend (page shell, esbuild bundle, xterm
// stylesheet). The directive embeds only files at or below this file's
// directory, which is exactly where `make frontend` (npm run build) writes
// them — hence the embed lives in package main, not internal/web.
//
// A fresh checkout has no bundle.js (it is generated and git-ignored), so a
// build that skips `make frontend` will not embed it and GET /bundle.js then
// 404s; the committed index.html keeps the glob non-empty so the package still
// compiles. CI and the image build run the frontend step first.
//
//go:embed static/*
var staticFiles embed.FS

// staticFS is the embedded frontend rooted at the static directory, so callers
// reference files as "index.html", "bundle.js", "xterm.css" without the prefix.
func staticFS() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// Unreachable: "static" is a literal directory embedded above, so Sub
		// never fails. Panicking documents that invariant rather than hiding it.
		panic(err)
	}
	return sub
}
