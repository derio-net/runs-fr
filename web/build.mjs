// Bundle the xterm.js frontend and stage every asset the Go binary embeds.
//
// Output lands in cmd/runs-fr/static/ — the one directory the `//go:embed`
// directive in package main can reach. We emit three files there:
//   bundle.js   the esbuild-bundled, minified app (TS -> one JS file)
//   xterm.css   vendored from @xterm/xterm so the page needs no CDN
//   index.html  the page shell, copied from web/index.html
//
// Run via `npm run build` (or `make frontend`).
import { build } from "esbuild";
import { copyFile, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const webDir = dirname(fileURLToPath(import.meta.url));
const outDir = resolve(webDir, "..", "cmd", "runs-fr", "static");

await mkdir(outDir, { recursive: true });

await build({
  entryPoints: [resolve(webDir, "src", "main.ts")],
  bundle: true,
  minify: true,
  sourcemap: false,
  format: "iife",
  target: "es2020",
  outfile: resolve(outDir, "bundle.js"),
  logLevel: "info",
});

await copyFile(
  resolve(webDir, "node_modules", "@xterm", "xterm", "css", "xterm.css"),
  resolve(outDir, "xterm.css"),
);
await copyFile(resolve(webDir, "index.html"), resolve(outDir, "index.html"));

console.log(`staged bundle.js, xterm.css, index.html -> ${outDir}`);
