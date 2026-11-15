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
R3. Every other module with a reachable vulnerability — at least `golang.org/x/net` and `github.com/moby/spdystream` — moves to a fixed version, `go mod tidy` leaves `go.mod`/`go.sum` clean, and CI's `go` job fails when it would not (`go mod tidy -diff`).
R4. `govulncheck ./...` reports zero reachable vulnerabilities at the PR head.
R5. `ci.yml` gains a `govulncheck` job that fails the workflow on any reachable vulnerability, with a pinned govulncheck version; it runs on every pull request and push the workflow already covers, under the same CI-authority gate as the existing jobs.
R6. The same workflow also runs weekly against `main` (`schedule`) and on demand (`workflow_dispatch`, so the scheduled path can be exercised without waiting); when a scheduled `govulncheck` run fails on GitHub, a schedule-only job opens — or comments on, if one is already open — a `govulncheck: reachable vulnerability on main` issue when the scan found a vulnerability (govulncheck exit 3), or a `govulncheck: scheduled scan failed on main` issue when the job failed otherwise, so a newly published advisory surfaces without anyone opening a PR and a broken scan is never reported as a vulnerability. Only that job holds `issues: write`.
R7. `golangci-lint` is pinned to a release that supports Go 1.27, and the lint step passes.
R8. A `renovate.json` at the repo root configures Renovate for gomod (with `go mod tidy` after updates), npm under `web/`, both Dockerfiles, and GitHub Actions, plus regex managers for the pinned pip packages in `test/e2e/Dockerfile`, the pinned govulncheck version and the pinned golangci-lint version in `ci.yml`; weekly schedule, a dependency dashboard, automerge off, and groups for `k8s.io/*`, `golang.org/x/*`, `@xterm/*`, GitHub Actions, and a `go toolchain` group that moves go.mod's `go`/`toolchain` lines, the Dockerfile's `golang` image and golangci-lint together.
R9. The gateway's behaviour is unchanged: PR CI builds the image (build only, no push), and attaching to a pod through the deployed gateway works on the bumped client-go.
R10. Nothing outside Go, its modules and golangci-lint is bumped by hand in this PR — Node, action majors, esbuild, xterm and the e2e pip pins are left for Renovate's first PRs.
R11. The CI test step and `make test` both report per-package test coverage (`-cover`), the baseline #32 asks to print.

## Design

### Toolchain (R1)

- `go.mod`: `go 1.27.0`, `toolchain go1.27.4`.
- `Dockerfile`: `FROM golang:1.27-alpine AS build`; the `:23` comment becomes
  "go.mod pins go 1.27".
- `ci.yml` `go` job: `actions/setup-go@v5` with `go-version-file: go.mod`
  `cache: true` kept. setup-go **v5** installs the `go` directive's version
  (1.27.0), and `go` then fetches the `toolchain` line's go1.27.4 itself
  (`GOTOOLCHAIN=auto`), so every command runs on go1.27.4 (the CI log's
  `go version` lines confirm it). The cost is a noisy cache restore that
  collides with the fetched toolchain. setup-go v6 reads `toolchain` directly;
  that major bump is Renovate's first github-actions PR (R10), not this PR's.

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

### Vulnerability gate (R3, R4, R5, R6, R11)

`ci.yml`:

- `on:` gains `schedule: [{cron: "0 6 * * 1"}]` (Monday 06:00 UTC) and
  `workflow_dispatch:`. A scheduled run executes the whole workflow on `main`;
  that is cheap, and it means the weekly run also catches a test or lint
  regression from a changed environment. `workflow_dispatch` lets the operator
  exercise the same path on demand. Neither interacts with the authority `if:`,
  which reads only `vars.CI_AUTHORITY` and `github.server_url`.
- New job `govulncheck`, same `if:` authority expression as `go`/`helm`:
  checkout (`persist-credentials: false`) → `actions/setup-go@v5`
  (`go-version-file: go.mod`) →
  `go install golang.org/x/vuln/cmd/govulncheck@<pinned vX.Y.Z>` → run the
  installed binary, publishing its exit code as the job output `exit`.
  Installed, not `go run`: `go run` reports every child failure as exit 1,
  which would hide govulncheck's exit 3 (vulnerabilities found) behind its
  exit 1 (tool error). govulncheck exits non-zero when it finds a reachable
  (symbol-level) vulnerability, so the default mode IS the hard gate; no
  `continue-on-error`. The job is `permissions: {contents: read}`. No Node
  step: the embed glob is satisfied by the tracked static files.
- **Surfacing a scheduled failure (R6).** A separate job `govulncheck-issue`,
  `needs: govulncheck`,
  `if: failure() && github.event_name == 'schedule' && github.server_url == 'https://github.com'`,
  `permissions: {issues: write}` and no checkout, reads
  `needs.govulncheck.outputs.exit`. On exit 3 its title is
  `govulncheck: reachable vulnerability on main`; on anything else (a checkout,
  setup or vuln-DB failure, or no output at all) it is
  `govulncheck: scheduled scan failed on main`. It runs `gh` to find an open
  issue with that exact title and comment the run URL on it, or create it if
  none is open. The scan job, which runs on every PR and push, never holds a
  write scope; the workflow default stays `contents: read`. A red PR or push run already
  surfaces on the PR/commit; the issue exists for the run nobody is watching.
  Known limit: GitHub disables a schedule after 60 days without repository
  activity; a merged Renovate PR resets that, and the weekly
  `acceptance-report` workflow shares the same exposure.
- `go` job's test step: `go test ./... -race -cover`; `Makefile`'s `test`
  target gets the same flag, keeping the local mirror in step (R11).
- `go` job gains a `go mod tidy -diff` step after checkout/setup-go, failing
  on any drift (R3) — this also guards Renovate PRs beyond `gomodTidy`.

### Image build on PRs (R9)

`build.yml` builds the image only on push to `main`, so a broken Dockerfile
(e.g. the `golang:1.27-alpine` stage) would first fail after merge. `ci.yml`
gains a job `image`, same authority `if:`, that runs
`docker/setup-buildx-action@v3` → `docker/build-push-action@v6` with
`context: .`, `push: false` — the same actions and majors `build.yml` already
uses (R10: no action-major changes here). It runs the full multi-stage build,
frontend included.

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
    {"groupName": "go toolchain", "matchPackageNames": ["go", "golang", "golangci/golangci-lint"]},
    {"matchManagers": ["gomod"], "matchDepTypes": ["golang"], "rangeStrategy": "bump"}
  ],
  "customManagers": [
    {"customType": "regex", "managerFilePatterns": ["/^test/e2e/Dockerfile$/"],
     "matchStrings": ["\"(?<depName>[a-zA-Z0-9_.-]+)==(?<currentValue>[^\"]+)\""],
     "datasourceTemplate": "pypi"},
    {"customType": "regex", "managerFilePatterns": ["/^\\.github/workflows/.+\\.ya?ml$/"],
     "matchStrings": ["golang\\.org/x/vuln/cmd/govulncheck@(?<currentValue>v[0-9.]+)"],
     "depNameTemplate": "golang.org/x/vuln", "datasourceTemplate": "go"},
    {"customType": "regex", "managerFilePatterns": ["/^\\.github/workflows/ci\\.ya?ml$/"],
     "matchStrings": ["golangci-lint-action@\\S+\\s+with:\\s+version:\\s*(?<currentValue>v[0-9.]+)"],
     "depNameTemplate": "golangci/golangci-lint", "datasourceTemplate": "github-releases"}
  ]
}
```

**Drift policy (R1, R8).** The invariant is that go.mod, the Docker build
stage and CI agree on the Go **minor**. CI reads go.mod, so it cannot drift.
The `go toolchain` group moves go.mod's `go` directive (depType `golang`,
`rangeStrategy: bump` so it advances on a new minor rather than staying a
floor), its `toolchain` line (depType `toolchain`), the Dockerfile's
`golang:<minor>-alpine` tag and the golangci-lint pin in one PR, so a new Go
minor never lands without a linter that understands it. A toolchain-only patch
bump (`go1.27.4` → `go1.27.5`) leaves the minor-only Docker tag unchanged,
which is consistent with the invariant (the tag floats to the newest 1.27.x).
The plan validates the file with `renovate-config-validator` AND a local
extraction dry run (`renovate --platform=local --dry-run=extract`, via
`npx --package renovate`) that must list deps from every manager, including the
three regex managers. Renovate PRs are verified by the same CI, including the
govulncheck gate.

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
- deps-gate-supported-toolchain: none — PR CI verifies it on the PR head: the `go` job (setup-go from go.mod, vet, test -race, lint) and the `image` job (the Docker build stage on golang:1.27-alpine, push: false)
- deps-gate-no-reachable-vulns: none — PR CI verifies it on the PR head: the `govulncheck` job (zero reachable vulns) and the `go` job's `go mod tidy -diff` step
- deps-gate-vuln-gate-blocks: live — the PR/push half is exercised pre-merge (the job runs on the PR, and the plan runs the same command against the pre-bump tree to show it exits non-zero), but a `schedule` trigger fires only from the merged default branch, so the weekly half and its issue-on-failure path are proven post-merge (Test Plan 3)
- deps-gate-dependency-bot: live — Renovate only acts on the merged default branch, so its PRs and dashboard can exist only after merge
- deps-gate-attach-works: live — needs the deployed gateway and a real pod; the staging-gate e2e runs only on a `main` build

## Test Plan

Post-merge — operator-driven:

1. Frank's staging-gate e2e (`ghcr.io/derio-net/runs-fr-smoke`) passes for the merged sha (build.yml `trigger-gate` notified, Tekton run green).
2. Attach to a pod through the deployed gateway: an interactive shell opens, typed input echoes, a terminal resize takes effect, and exit closes the session cleanly.
3. Run the `ci` workflow on `main` via `workflow_dispatch` and confirm the `govulncheck` job ran and passed; within a week of merge, confirm a `schedule`-triggered `ci` run on `main` ran the `govulncheck` job.
4. Within a week of merge: the "Dependency Dashboard (runs-fr)" issue exists and lists dependencies from all four ecosystems (gomod, npm, dockerfile, github-actions); at least one Renovate PR has opened and its CI ran the `govulncheck` job.

## Implementation Plans

| Plan | Repo | File | Depends on |
|------|------|------|------------|
| 2026-11-15-runs-fr-deps-gate | `derio-net/runs-fr` | `2026-11-15-runs-fr-deps-gate` | — |
