# runs-fr: supported Go toolchain, govulncheck gate, dependency bot

Batch `runs-fr-deps-gate` — derio-net/runs-fr#31, #32, #37, one PR.

## Context

The gateway is a shell gateway (WebSocket in, Kubernetes exec out) and it is
built on an end-of-life Go:

- `go.mod` — `go 1.23.0` / `toolchain go1.23.4`
- `Dockerfile:25` — `FROM golang:1.23-alpine AS build` (comment at `:23` says "go.mod pins go 1.23")
- `.github/workflows/ci.yml:20-22` — `actions/setup-go@v5` with `go-version: "1.23"`

The 2026-06-10 audit (`docs/audits/2026-06-10-repo-audit.md`, SEC-1/DEP-1/DEP-2)
found 31 vulnerabilities reachable from this code — stdlib `net/http`,
`crypto/tls`, `crypto/x509`, plus `golang.org/x/net@v0.30.0` and
`github.com/moby/spdystream@v0.5.0` (the SPDY exec fallback). Nothing in
`.github/` runs `govulncheck` (OPS-1), and no Renovate or Dependabot config
exists (DEP-3), so staleness recurs structurally.

Current at the time of writing: Go 1.27.4 (supported minors 1.26, 1.27);
`k8s.io/client-go` v0.37.2 (repo is on v0.32.13). The exec path most exposed to
client-go changes is `internal/term/exec.go:50-58` — a WebSocket executor with
an SPDY fallback via `remotecommand.NewFallbackExecutor` — consumed by
`internal/term/attacher.go:62` (`StreamWithContext`).

`cmd/runs-fr/embed.go:18` embeds `static/*`; `static/index.html` and
`static/xterm.css` are tracked, so the package compiles without the
git-ignored `bundle.js` — a vulnerability scan needs no frontend build.

The derio-net org has the Renovate GitHub app installed (frank's
`renovate.json5` runs it with `automerge: false`).

## Requirements

R1. `go.mod` declares `go 1.27.0` and `toolchain go1.27.4`; the Dockerfile build stage uses `golang:1.27-alpine`; `ci.yml` reads the Go version from `go.mod` (`go-version-file: go.mod`) instead of a literal, so the three cannot drift.
R2. `k8s.io/api`, `k8s.io/apimachinery` and `k8s.io/client-go` move from v0.32.13 to the latest v0.37.x one minor at a time, one commit per rung, with `go vet ./...` and `go test ./... -race` green at every rung.
R3. Every other module with a reachable vulnerability — at least `golang.org/x/net` and `github.com/moby/spdystream` — moves to a fixed version, and `go mod tidy` leaves `go.mod`/`go.sum` clean.
R4. `govulncheck ./...` reports zero reachable vulnerabilities at the PR head.
R5. `ci.yml` gains a `govulncheck` job that fails the workflow on any reachable vulnerability, with a pinned govulncheck version; it runs on every pull request and push the workflow already covers, and on a weekly schedule against `main`, under the same CI-authority gate as the existing jobs.
R6. The CI test step reports per-package test coverage (`-cover`), the baseline #32 asks to print.
R7. `golangci-lint` is pinned to a release that supports Go 1.27, and the lint step passes.
R8. A `renovate.json` at the repo root configures Renovate for gomod (with `go mod tidy` after updates), npm under `web/`, both Dockerfiles, and GitHub Actions, plus regex managers for the pinned pip packages in `test/e2e/Dockerfile` and the pinned govulncheck version in `ci.yml`; weekly schedule, a dependency dashboard, automerge off, and groups for `k8s.io/*`, `golang.org/x/*`, `@xterm/*` and GitHub Actions.
R9. The gateway's behaviour is unchanged: the image builds, and attaching to a pod through the deployed gateway works on the bumped client-go.
R10. Nothing outside Go, its modules and golangci-lint is bumped by hand in this PR — Node, action majors, esbuild, xterm and the e2e pip pins are left for Renovate's first PRs.

## Design

### Toolchain (R1)

- `go.mod`: `go 1.27.0`, `toolchain go1.27.4`.
- `Dockerfile`: `FROM golang:1.27-alpine AS build`; the `:23` comment becomes
  "go.mod pins go 1.27".
- `ci.yml` `go` job: `actions/setup-go@v5` with `go-version-file: go.mod`
  (setup-go honours the `toolchain` line), `cache: true` kept.

The toolchain bump goes first, before any module move: most of the 31 findings
are stdlib and clear on it alone, and the module ladder then runs on a
supported compiler.

### Module ladder (R2, R3)

One commit per rung, each green on `go vet ./...` and `go test ./... -race`:

1. toolchain (R1)
2. `k8s.io/{api,apimachinery,client-go}@v0.33.x` (latest patch of the minor)
3. `@v0.34.x`
4. `@v0.35.x`
5. `@v0.36.x`
6. `@v0.37.x`
7. `go get golang.org/x/net@latest github.com/moby/spdystream@latest` plus any
   module `govulncheck` still names, then `go mod tidy`

The three k8s modules always move together (they are released in lockstep and
mixing minors is unsupported). If a rung breaks the build or a test, the fix
lands in that rung's commit — the commit stays green. `internal/term/exec.go`
and `internal/term/attacher.go` are the expected touch points; any API change
there is adapted, never worked around by pinning below the rung.

### Vulnerability gate (R4, R5, R6)

`ci.yml`:

- `on:` gains `schedule: [{cron: "0 6 * * 1"}]` (Monday 06:00 UTC) and
  `workflow_dispatch:`. A scheduled run executes the whole workflow on `main`;
  that is cheap, and it means the weekly run also catches a test or lint
  regression from a changed environment.
- New job `govulncheck`, same `if:` authority expression as `go`/`helm`:
  checkout → `actions/setup-go@v5` (`go-version-file: go.mod`) →
  `go run golang.org/x/vuln/cmd/govulncheck@<pinned vX.Y.Z> ./...`.
  govulncheck exits non-zero when it finds a reachable (symbol-level)
  vulnerability, so the default mode IS the hard gate; no `continue-on-error`.
  No Node step: the embed glob is satisfied by the tracked static files.
- `go` job's test step: `go test ./... -race -cover`.

### golangci-lint (R7)

Bump `golangci-lint-action@v9`'s `version:` from `v2.3` to the newest v2.x
whose release notes list Go 1.27 support. `.golangci.yml` is unchanged unless
the new version reports findings; real findings are fixed, not disabled.

### Renovate (R8)

`renovate.json`:

```json
{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "extends": ["config:recommended"],
  "schedule": ["before 6am on monday"],
  "timezone": "Etc/UTC",
  "automerge": false,
  "dependencyDashboard": true,
  "dependencyDashboardTitle": "Dependency Dashboard (runs-fr)",
  "postUpdateOptions": ["gomodTidy"],
  "enabledManagers": ["gomod", "npm", "dockerfile", "github-actions", "custom.regex"],
  "packageRules": [
    {"groupName": "k8s.io", "matchManagers": ["gomod"], "matchPackageNames": ["k8s.io/**"]},
    {"groupName": "golang.org/x", "matchManagers": ["gomod"], "matchPackageNames": ["golang.org/x/**"]},
    {"groupName": "xterm", "matchManagers": ["npm"], "matchPackageNames": ["@xterm/**"]},
    {"groupName": "github-actions", "matchManagers": ["github-actions"]},
    {"groupName": "go toolchain", "matchDatasources": ["golang-version", "docker"], "matchPackageNames": ["go", "golang"]}
  ],
  "customManagers": [
    {"customType": "regex", "managerFilePatterns": ["/^test/e2e/Dockerfile$/"],
     "matchStrings": ["\"(?<depName>[a-zA-Z0-9_.-]+)==(?<currentValue>[^\"]+)\""],
     "datasourceTemplate": "pypi"},
    {"customType": "regex", "managerFilePatterns": ["/^\\.github/workflows/.+\\.ya?ml$/"],
     "matchStrings": ["golang\\.org/x/vuln/cmd/govulncheck@(?<currentValue>v[0-9.]+)"],
     "depNameTemplate": "golang.org/x/vuln", "datasourceTemplate": "go"}
  ]
}
```

The `go toolchain` group keeps `go.mod`'s `go`/`toolchain` lines and the
Dockerfile's `golang` image in one PR, so R1's "cannot drift" survives updates.
The file is validated in the plan with `renovate-config-validator` (run via
`npx --package renovate`). Renovate PRs are verified by the same CI, including
the govulncheck gate.

Renovate only reads a repo the app can see. If the derio-net installation is
repo-selected rather than all-repos, enabling it for runs-fr is an operator
step in the GitHub app settings — recorded as a back-loaded manual phase.

### Out of scope (R10)

Node 22, mixed action majors (`checkout@v4` vs `@v7`), esbuild 0.25, xterm 5,
the e2e image's pip pins, GitHub Dependabot security updates and secret
scanning (both disabled in repo settings). Renovate's first PRs and the
dashboard cover the first four.

## Verification

gate: tester
- deps-gate-supported-toolchain: none — PR CI (`go` job: vet, test -race, lint) verifies it on the PR head
- deps-gate-no-reachable-vulns: none — PR CI's `govulncheck` job verifies it on the PR head
- deps-gate-vuln-gate-blocks: none — PR CI runs the job; its fail-on-find behaviour is govulncheck's default exit code, exercised in the plan by a local run against the pre-bump tree
- deps-gate-dependency-bot: live — Renovate only acts on the merged default branch, so its PRs and dashboard can exist only after merge
- deps-gate-attach-works: live — needs the deployed gateway and a real pod; the staging-gate e2e runs only on a `main` build

## Test Plan

Post-merge — operator-driven:

1. Frank's staging-gate e2e (`ghcr.io/derio-net/runs-fr-smoke`) passes for the merged sha (build.yml `trigger-gate` notified, Tekton run green).
2. Attach to a pod through the deployed gateway: an interactive shell opens, typed input echoes, a terminal resize takes effect, and exit closes the session cleanly.
3. Within a week of merge: the "Dependency Dashboard (runs-fr)" issue exists and lists dependencies from all four ecosystems (gomod, npm, dockerfile, github-actions); at least one Renovate PR has opened and its CI ran the `govulncheck` job.
