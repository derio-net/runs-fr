# runs-fr — Repository Audit & Improvement Plan

**Date:** 2026-06-10
**Auditor:** Clawdia (principal-engineer audit, analysis only — no code modified)
**Scope:** full repo at `origin/main` (commit `ec6b28a`), ~2,000 lines of Go + a small TS frontend, Helm chart, CI. Security dimension given full depth (the project is a web shell gateway). Every Go source and test file, both workflows, the Dockerfile, and all chart templates were read in full; `charts/runs-fr/test/assert.sh` was skimmed (header + structure) and the frontend lockfile was not reviewed line-by-line — these are the only lighter-reviewed areas.

---

## Executive Summary

**Health grade: B+.** This is an unusually disciplined codebase for its age: small, idiomatic Go with deliberate module boundaries, a loudly and honestly documented trust model, hardened packaging (distroless, nonroot, read-only rootfs, namespace-scoped RBAC), golden-file chart assertions in CI, and tests that assert behaviour rather than execution. What keeps it off an A is operational, not structural: the toolchain is pinned to **EOL Go 1.23.4**, which (with two stale modules) leaves **31 reachable vulnerabilities** flagged by govulncheck in the exact packages a shell gateway lives on (`net/http`, `crypto/tls`, `crypto/x509`, `x/net`, `moby/spdystream`); the gateway performs **no authorization and writes no audit trail** — any request bearing a non-empty trusted header reaches `pods/exec`, and nobody can later answer "who attached to which pod"; and the live exec bridge (`internal/term/attacher.go`, 37.9% coverage) has never been verified end-to-end (Phase 8, issue #9, still open).

**Top 3 risks:** (1) EOL toolchain + stale deps with reachable CVEs on the TLS/HTTP path; (2) authenticated-but-unauthorized, unaudited access to `pods/exec`; (3) the core exec bridge is the least-tested, never-cluster-verified code in the repo.

**Top 3 opportunities:** (1) automate dependency hygiene (toolchain bump + Renovate + govulncheck CI gate) — one afternoon erases the entire High-severity column; (2) propagate the forward-auth user into structured audit logs (the identity is already on every request, it's just dropped on the floor); (3) an integration test harness (envtest/kind) for the exec path, converting the standing manual Phase-8 verification into CI.

---

## Phase 1 — Repo Map

**Purpose.** Kubernetes-native, cluster-agnostic web shell gateway ("Component C" of the k8s-native agentic-runs design, spec in `derio-net/frank`). Lists live agent-run pods by an `fr.run/*` label selector and bridges a browser xterm.js terminal to a pod's tmux session over the `pods/exec` subresource. Maturity: **walking skeleton, pre-first-deploy** — 7 of 8 plan phases merged; Phase 8 (manual cluster verification) open as issue #9.

**Stack.** Go 1.23 (`go.mod:3-5`), `k8s.io/client-go` v0.32.13 + `github.com/coder/websocket` v1.8.14 (`go.mod:8-11`); TypeScript frontend on `@xterm/xterm` + `@xterm/addon-fit`, bundled by esbuild (`web/build.mjs`) and embedded via `//go:embed` (`cmd/runs-fr/embed.go:17`); distroless-static container (`Dockerfile:38-42`); Helm chart with namespace-scoped RBAC (`charts/runs-fr/templates/role.yaml`).

**Architecture / flow.**

```
browser ──(forward-auth proxy sets X-Forwarded-User)──> server.New mux
  /healthz /readyz /{$} /bundle.js /xterm.css   ── ungated
  /api/runs            ── RequireForwardAuth → runs.Lister.List (pods list)
  /api/runs/{id}/attach ── RequireForwardAuth → regex-validate id
        → runs.Lister.Resolve (label fr.run/id=<id> → pod name)
        → websocket.Accept → term.KubeAttacher.Attach
        → remotecommand Fallback(WebSocket→SPDY) exec: tmux attach -t <id>
        → term.session demuxes ws frames ⇄ exec stdin/stdout/resize
```

**Key directories.**

| Path | One-liner |
|---|---|
| `cmd/runs-fr/` | main wiring + `embed.FS` of the built frontend |
| `internal/config/` | env-var config loader (NAMESPACE required) |
| `internal/kube/` | rest.Config resolution (in-cluster → kubeconfig), clientset, selector, readiness probe |
| `internal/runs/` | pod→Run projection; `List` and `Resolve` |
| `internal/server/` | HTTP mux, forward-auth middleware, attach handler, static assets + CSP |
| `internal/term/` | exec URL builder, fallback executor, ws⇄exec session demux, ping loop |
| `web/` | TS frontend source + esbuild script |
| `charts/runs-fr/` | Helm chart + golden-assertion test script |
| `.github/workflows/` | `ci.yml` (vet/test/lint/helm), `release.yml` (GHCR image on `v*` tags) |
| `docs/superpowers/plans/` | fr plan that built the repo (8 phases) |

**Surprises (positive):** the security contract is the *first* section of the README (`README.md:25-50`) and is repeated at every wiring site (`internal/server/server.go:82-88`, `charts/runs-fr/values.yaml:3-13`); the chart test script asserts security invariants (no ClusterRole, exec verb, hardened pod) rather than just `helm lint`; the frontend deliberately avoids `@xterm/addon-attach` with a comment explaining the protocol mismatch (`web/src/main.ts:10-12`). **Surprise (negative):** the toolchain is 18 months old in a repo created two days ago — `go 1.23.0 / toolchain go1.23.4` (`go.mod:3-5`) and `golang:1.23-alpine` (`Dockerfile:26`) were stale at creation time.

---

## Phase 2 — Audit Report

Severity legend: **C**ritical / **H**igh / **M**edium / **L**ow. Each finding labelled **[fact]** or **[judgment]**.

### Security

**SEC-1 (High, fact) — EOL Go toolchain + two stale modules ⇒ 31 reachable vulnerabilities.**
`go.mod:3-5` pins `go 1.23.0` / `toolchain go1.23.4`; `Dockerfile:26` builds with `golang:1.23-alpine`; `ci.yml:18-20` sets up Go "1.23". Go 1.23 left upstream support in 2025; security patches stopped at 1.23.x. `govulncheck ./...` (run 2026-06-10 on this checkout) reports **31 vulnerabilities reachable from this code**, including: `GO-2025-3751` / `GO-2025-3420` (sensitive headers sent across redirects in `net/http` — directly relevant, since the gateway's outbound client carries ServiceAccount bearer tokens to the API server via `internal/runs/lister.go:63` and `internal/term/exec.go:44`), `GO-2025-4012` (cookie-parsing memory exhaustion in `net/http`, reachable from the public listener `cmd/runs-fr/main.go:81`), multiple `crypto/tls`/`crypto/x509` issues on the exec TLS path, `GO-2026-5026`/`GO-2026-4918` in `golang.org/x/net@v0.30.0` (fixed v0.53/v0.55; `go.sum`), and `GO-2026-4958` in `github.com/moby/spdystream@v0.5.0` (fixed v0.5.1 — this is the SPDY exec fallback transport, `internal/term/exec.go:50`). Consequence: known-vulnerable TLS/HTTP code on both the browser-facing and apiserver-facing legs of a shell gateway. This is the single most important finding in the audit.

**SEC-2 (Medium, fact) — Authentication without authorization, and no audit trail.**
`internal/server/auth.go:17` gates on `r.Header.Get(header) == ""` — the *value* of the trusted identity header is never read again anywhere in the codebase. Consequences: (a) **any** user the upstream proxy authenticates — regardless of who — can list runs and attach a shell to every run pod (no allowlist, no group check); (b) attach lifecycle is logged only at `Debug` without the user (`internal/server/attach.go:80,96`), and the default `slog` level is Info, so production logs contain **zero record of who attached to which pod when**. For a tool whose own README says "treat reaching this Service as equivalent to a shell in every run pod" (`README.md:47`), the absence of an audit log is the most significant gap after SEC-1. [judgment on severity: Medium not High only because the deployment target is a single-operator homelab and the proxy (Authentik) keeps its own access log.]

**SEC-3 (Medium, fact) — No `ReadHeaderTimeout` on the listener.**
`cmd/runs-fr/main.go:67-77` constructs `http.Server{Addr, Handler}` with no timeouts. A client can hold connections open indefinitely while trickling headers (Slowloris). Global read/write timeouts are *correctly* absent (they would kill long-lived attach websockets), but `ReadHeaderTimeout` is safe and free. Mitigated by ClusterIP + proxy posture, but it's a one-line fix.

**SEC-4 (Low, fact) — Cross-site WebSocket hijacking protection is implicit.**
`internal/server/attach.go:79` calls `websocket.Accept(w, r, nil)`. coder/websocket's nil-options default *does* enforce same-origin (it rejects mismatched `Origin` headers), so CSWSH — the classic way to ride an authenticated browser session past a forward-auth proxy — is blocked today. But the protection is an undocumented library default: a future `AcceptOptions{InsecureSkipVerify: true}` "to fix a dev CORS issue" would silently remove the only origin check. Worth pinning with an explicit option + comment + test.

**SEC-5 (Low, judgment) — No bound on concurrent attach sessions.**
Each accepted attach holds an exec stream, a ping goroutine, and a websocket (`internal/server/attach.go:62-98`, `internal/term/attacher.go:42-63`) with no per-user or global cap. A misbehaving (authenticated) client can open sessions until the 128Mi pod limit (`charts/runs-fr/values.yaml:80`) OOMs the gateway. Acceptable at homelab scale; revisit before multi-user use.

**SEC-6 (Low, judgment) — Identity header travels plaintext inside the cluster.**
The proxy→gateway hop is HTTP; anyone on-path in-cluster could read or inject the trusted header. The opt-in default-deny NetworkPolicy (`charts/runs-fr/templates/networkpolicy.yaml`) constrains *sources* but not on-path observation. Fine for a single-tenant homelab CNI; note mTLS/mesh if that changes. NetworkPolicy being opt-in rather than default is a defensible documented choice (`values.yaml:64-66`).

**Security facts that are *right* (worth preserving):** run-id input is regex-constrained before any k8s use (`internal/server/attach.go:18`, tested at `attach_test.go:73-88`); the exec command is an argv array, not a shell string — no injection surface (`internal/term/exec.go:30`); RBAC is a namespace-scoped Role with exactly `pods get/list/watch` + `pods/exec create`, asserted in CI (`charts/runs-fr/templates/role.yaml:1-2`, `charts/runs-fr/test/assert.sh:6-10`); the pod is nonroot/read-only/no-caps (`charts/runs-fr/templates/deployment.yaml:33-46`); static assets ship a tight CSP + nosniff (`internal/server/static.go:14-22`); no secrets anywhere in the repo (verified by search).

### Architecture & design

Healthy overall: small packages with single responsibilities, dependencies injected via an options pattern (`internal/server/server.go:37-52`), interfaces defined at consumer boundaries (`RunLister`, `RunResolver`, `Attacher`), no circular deps, no god files (largest file is 143 lines).

**ARCH-1 (Medium, fact) — `Resolve` hardcodes the `fr.run/id` label while listing is configurable.**
`LABEL_SELECTOR` is configuration (`internal/config/config.go:23`, README table), but `Lister.Resolve` queries `labelID + "=" + id` with `labelID = "fr.run/id"` fixed (`internal/runs/lister.go:24,63`), and `tmuxSessionName(id) == id` is a hardcoded convention (`internal/server/attach.go:54`). A deployment that sets a different label scheme will *list* runs it can never *attach* — the portability promise ("the same image/chart installs into any cluster", `README.md:9-11`) is half-true. The comment at `lister.go:58-61` shows it's deliberate, but the run-harness contract (label key + tmux-session-named-after-id) is an undocumented cross-repo invariant.

**ARCH-2 (Low, fact) — `rest.Config` is resolved twice.**
`main.go:47-56` calls `kube.RestConfig(cfg)` and then `kube.NewClientset(cfg)`, which calls `RestConfig` again internally (`internal/kube/client.go:57-69`). The attacher's transport credentials and the clientset could in principle diverge (env mutation between calls); mostly it's just redundant. `NewClientset` should accept the already-resolved `*rest.Config`.

**ARCH-3 (Low, fact) — `index.html` exists in two committed copies.**
`web/index.html` (source) and `cmd/runs-fr/static/index.html` (build output, committed so the embed glob stays non-empty — `cmd/runs-fr/embed.go:8-15`). They are byte-identical today, but a `go build` that skips `make frontend` embeds and serves the *stale committed copy* silently. Drift will be invisible until someone notices the wrong page in production.

### Code quality

Excellent: gofmt-clean, comment density is exceptional and the comments explain *why* (e.g., the frame-type demux rationale at `internal/term/session.go:36-42`, the `GET /{$}` non-catch-all note at `static.go:50-52`). No dead code, no duplication beyond ARCH-3, error handling is consistent (`fmt.Errorf` with package prefixes, sentinel `ErrNotFound` mapped to 404 at `attach.go:66`).

**CQ-1 (Low, judgment)** — `handleControl`'s drop-stale-then-push triple-`select` (`internal/term/session.go:106-117`) is correct (and tested: `session_test.go:88-110`) but baroque; a small comment-free reader will stumble. Not worth churn on its own.

### Testing

`go test ./...` passes (run on this checkout). Coverage: config 100%, server 90.0%, runs 85.7%, **kube 44.4%, term 37.9%, cmd 0%**. Tests assert behaviour, not execution — e.g., `TestAttach_InvalidID400` asserts the resolver was *never called* (`internal/server/attach_test.go:84-86`), and `TestAttach_ContextCancelStopsAttacher` proves no goroutine leak (`attach_test.go:154-193`).

**TEST-1 (Medium, fact) — The core bridge is the least-tested code.**
`internal/term/attacher.go` (the real exec path: executor construction, ping loop, dead-peer teardown) has no unit tests — the file's own comment defers to "Phase 8 manual verification" (`attacher.go:19-21`), and **Phase 8 (issue #9) is still open**: per repo state, the live exec path has never been verified against a cluster. The riskiest code (network bridging, three concurrent failure sources) has the least automated safety net.

**TEST-2 (Low, fact) — No integration/e2e layer and zero frontend tests.** No envtest/kind harness for the exec round-trip; `web/src/main.ts` (the hand-written ws pump that must mirror the server demux exactly, per its own header comment) is untested. Acceptable at this maturity; the protocol-mirroring claim is exactly the kind of invariant a contract test should pin.

### Performance

Healthy for the workload (one operator, a handful of runs). Static assets are served from the in-memory embed FS; the per-request `fs.ReadFile` (`internal/server/static.go:62`) is a non-issue. The capacity-1 resize channel prevents control-frame backpressure (`session.go:60`). Only nit (Low, fact): no `Cache-Control` headers on static assets — every page load refetches the ~300KB bundle through the proxy.

### Dependencies

**DEP-1 (High, fact)** — covered by SEC-1: toolchain EOL; `golang.org/x/net v0.30.0` and `moby/spdystream v0.5.0` have known fixed vulns (`go.mod:36`, `go.sum`). **DEP-2 (Medium, judgment)** — `k8s.io/* v0.32.13` (`go.mod:9-11`): the 0.32 line is at/near the end of its upstream support window in mid-2026; plan the ladder to a supported minor. **DEP-3 (Low, fact)** — no Renovate/Dependabot config anywhere in the repo, so staleness will recur structurally. Direct dep count is admirably small (4); licenses are Apache-2.0/MIT/BSD — no risk.

### DevEx & operations

Strong baseline: one-command `make build/test/lint/frontend` (`Makefile`), CI runs frontend→vet→test(-race)→golangci-lint v2 + helm golden assertions (`ci.yml`), release workflow publishes triple-tagged images with build cache (`release.yml:38-52`).

**OPS-1 (Medium, fact) — No vulnerability scanning anywhere in CI.** Neither `ci.yml` nor `release.yml` runs govulncheck or an image scanner — which is exactly how SEC-1 accumulated unnoticed in a 2-day-old repo.
**OPS-2 (Low, fact) — No log-level/format configuration.** `slog.Default()` text handler at Info (`main.go:28`); all attach/probe diagnostics are `Debug` and therefore invisible in production (`server.go:101,113`; `attach.go:80,96`). Combined with SEC-2 this means near-silent operation.
**OPS-3 (Low, fact) — The chart is not published.** `release.yml` ships only the image; chart installs are path-based. Fine while the only consumer is the `frank` umbrella repo (per `README.md:118-121`).

### Documentation

Excellent — README is accurate against the code on every claim spot-checked (config table ↔ `config.go:31-38`; route gating ↔ `server.go:70-92`; image tags ↔ `release.yml:38-45`), the security contract is impossible to miss, and `values.yaml` doubles as security documentation. Only gap (Low, fact): the run-harness contract from ARCH-1 (label key + tmux session naming) is documented nowhere a harness author would find it.

### Strengths summary

1. Trust model honestly stated and re-stated at every layer (README, values.yaml, code comments, chart NOTES).
2. Defense-in-depth packaging: distroless nonroot, read-only rootfs, drop-ALL caps, namespace-scoped Role, opt-in default-deny NetworkPolicy — with CI assertions guarding the invariants (`charts/runs-fr/test/assert.sh`).
3. Clean dependency-injected architecture; interfaces sized to consumers; zero framework bloat.
4. Behaviour-asserting tests with explicit negative cases (auth absent/empty, invalid id, no-catch-all static, goroutine-leak check).
5. Comment quality a tier above the norm — rationale, not narration.

---

## Phase 3 — Improvement Strategy

**Theme 1 — Dependency hygiene is the whole High-severity column.**
*Target state:* supported Go toolchain; all reachable govulncheck findings zero; staleness can't silently recur. *Principle:* a shell gateway's TLS/HTTP stack must track upstream security patches automatically — this is a process fix (CI gate + Renovate), not a one-time bump.

**Theme 2 — Identity is collected, then discarded.**
*Target state:* the forward-auth user is parsed once into the request context, every `/api/*` access and every attach open/close is logged at Info with `user`, `run id`, `pod`, duration; optionally a simple allowlist hook. *Principle:* for privileged access systems, *accountability* (who did what) is the cheapest 80% of authorization — buy it first.

**Theme 3 — The riskiest code has the weakest safety net.**
*Target state:* `internal/term` ≥ 75% coverage (ping loop + attach teardown unit-tested against the `wsConn` fake), plus one scripted integration check (kind or envtest) replacing the standing manual Phase-8 verification. *Principle:* test depth proportional to blast radius, not to ease of testing.

**Theme 4 — Implicit contracts (library defaults and cross-repo conventions) should be explicit.**
*Target state:* origin policy pinned in `AcceptOptions` with a test; the run-harness contract (label key, tmux session = run id) documented in the README and the `Resolve`/`tmuxSessionName` coupling either configured or explicitly frozen. *Principle:* invariants you depend on but don't state are the ones that break in refactors.

**Explicitly NOT recommending:** per-user RBAC/authz engine (single-operator deployment; the allowlist hook in Theme 2 is the right ceiling for now); mTLS/service mesh for the in-cluster hop (homelab CNI, NetworkPolicy suffices); session/connection quotas (SEC-5) beyond a trivial cap (no multi-tenant pressure); chart repository publishing (one consumer, path install works); frontend test framework (the contract test in Theme 3/4 covers the real risk); refactoring `handleControl` (correct + tested; churn > payoff).

**Definition of done:** `govulncheck ./...` clean and enforced in CI; Renovate PRs flowing; production logs answer "who attached to run X" for any session; `internal/term` ≥ 75% / `internal/kube` ≥ 70% coverage; one CI-runnable end-to-end exec check; an explicit origin-check test; zero High findings on re-audit.

---

## Phase 4 — Task Plan

### Quick wins (do immediately)

| ID | Task | Effort |
|---|---|---|
| QW-1 | Set `ReadHeaderTimeout: 10 * time.Second` on the `http.Server` (`cmd/runs-fr/main.go:67`) | S |
| QW-2 | Add a `govulncheck` job to `ci.yml` (continue-on-error first, hard gate after T-1 lands) | S |
| QW-3 | Log attach open/close at **Info** with `user` (from the auth header), `id`, `pod`, `duration` (`internal/server/attach.go:62-98`) | S |
| QW-4 | Pin the websocket origin policy: explicit `&websocket.AcceptOptions{}` + comment + a cross-origin-rejected test (`attach.go:79`) | S |
| QW-5 | Document the run-harness contract (label `fr.run/id`, tmux session named after run id) in the README | S |

### Milestone 0 — Safety net

| ID | Task | Files | Acceptance | Effort | Risk | Deps |
|---|---|---|---|---|---|---|
| T-0 | QW-2 (govulncheck CI, report-only) + capture current coverage numbers as the baseline in CI output | `.github/workflows/ci.yml` | CI prints vuln + coverage report on every PR | S | None | — |

### Milestone 1 — Critical fixes (security & correctness)

| ID | Task | Files | Acceptance | Effort | Risk | Deps |
|---|---|---|---|---|---|---|
| T-1 | **Toolchain + dependency bump**: Go → current supported minor (1.25+); `golang.org/x/net` ≥ 0.55; `moby/spdystream` ≥ 0.5.1; `k8s.io/*` → supported minor (ladder 0.32→0.33→0.34, testing at each rung); update `Dockerfile:26` and `ci.yml:20` | `go.mod`, `go.sum`, `Dockerfile`, `.github/workflows/ci.yml` | `govulncheck ./...` reports 0 reachable vulns; tests pass; image builds; manual attach smoke-test against a cluster | M | Medium — client-go minor bumps can shift exec/remotecommand behaviour; the fallback executor (`exec.go:39-53`) is the area to watch | T-0 |
| T-2 | **Audit logging + user propagation**: middleware extracts the auth-header value into the request context; `/api/*` access and attach open/close logged at Info (QW-3 done properly); add `LOG_LEVEL` env | `internal/server/auth.go`, `attach.go`, `server.go`, `internal/config/config.go`, chart `deployment.yaml` | Log lines answer "who attached to which pod, when, for how long"; tests assert user appears in context | M | Low | — |
| T-3 | QW-1 + QW-4 (listener timeout, explicit origin policy) | `cmd/runs-fr/main.go`, `internal/server/attach.go` + test | New cross-origin test red-then-green; header timeout set | S | Low | — |
| T-4 | Flip the T-0 govulncheck job to a hard CI gate | `ci.yml` | CI fails on reachable vulns | S | Low | T-1 |

### Milestone 2 — High-leverage improvements

| ID | Task | Files | Acceptance | Effort | Risk | Deps |
|---|---|---|---|---|---|---|
| T-5 | **Test the bridge**: unit-test `KubeAttacher.pingLoop` (dead-peer cancels session) and `Attach` teardown against the existing `wsConn` fake; cover `kube.Readiness` and `NewClientset` error paths | `internal/term/attacher*.go`, `internal/kube/client_test.go` | `internal/term` ≥ 75%, `internal/kube` ≥ 70% coverage | M | Low | — |
| T-6 | **Integration check**: scripted end-to-end exec round-trip against a kind cluster (deploy chart, create a labelled tmux pod, attach, echo, assert) — closes the standing Phase-8 manual gap; optional CI job (kind in GH Actions) | new `test/e2e/` or `hack/` script, possibly `ci.yml` | One command proves list→resolve→attach→stdin/stdout on a real cluster | L | Low (additive) | T-1 |
| T-7 | Renovate (or Dependabot) config for gomod, npm, Dockerfile, GH Actions | `renovate.json` | Bot PRs open for stale deps | S | None | T-4 |
| T-8 | Resolve the ARCH-1 portability gap: either derive `Resolve`'s label key from config or document `fr.run/id` as a frozen protocol constant and remove the implication that `LABEL_SELECTOR` changes the attach contract | `internal/runs/lister.go`, `README.md` | Decision recorded; behaviour and docs agree | S–M | Low | Open Q1 |

### Milestone 3 — Quality & polish

| ID | Task | Files | Acceptance | Effort | Risk |
|---|---|---|---|---|---|
| T-9 | `NewClientset(*rest.Config)` — resolve config once (ARCH-2) | `internal/kube/client.go`, `cmd/runs-fr/main.go` | Single `RestConfig` call in main | S | Low |
| T-10 | Single-source `index.html`: embed a placeholder-aware build check or fail `go build` loudly when `bundle.js` is missing rather than serving stale shell (ARCH-3) | `cmd/runs-fr/embed.go`, `web/build.mjs`, `Makefile` | Stale-shell drift impossible or loudly detected | S | Low |
| T-11 | `Cache-Control` (immutable, hashed filename for bundle) on static assets | `internal/server/static.go`, `web/build.mjs` | Repeat loads served from browser cache | M | Low |
| T-12 | Publish the chart (GHCR OCI) from `release.yml` when the second consumer appears | `.github/workflows/release.yml` | `helm install oci://…` works | S | Low |
| T-13 | Optional global attach-session cap (SEC-5) | `internal/server/attach.go` | 503 over the cap; test | S | Low |

### Implementation sketches — top 3 tasks

**T-1 (toolchain/deps).** Bump `go.mod` `go`/`toolchain` directives first and run the full suite — stdlib fixes (the bulk of the 31) come free. Then `go get golang.org/x/net@latest github.com/moby/spdystream@latest && go mod tidy`. For `k8s.io/*`, move one minor at a time (`v0.33.x`, then latest supported), re-running `go test ./...` and specifically exercising `internal/term/exec_test.go` — `remotecommand.NewFallbackExecutor` and the websocket executor are the components most likely to shift between client-go minors; check release notes for exec-protocol changes. Update `Dockerfile:26` (`golang:1.2X-alpine`) and `ci.yml` `go-version` in the same PR so CI and image can't diverge. Gotcha: `golangci-lint` pinned at v2.3 (`ci.yml:39`) may need a bump for the new Go version's AST.

**T-2 (audit logging).** In `RequireForwardAuth`, on success do `ctx := context.WithValue(r.Context(), userKey{}, val)` and pass `r.WithContext(ctx)`; add a tiny `UserFromContext` helper. In `handleAttach`, log `Info("attach opened", user, id, pod)` before `Attach` and `Info("attach closed", user, id, pod, duration, err)` after; add an Info access log for `/api/runs`. Wire `LOG_LEVEL` through `config.Load` into a `slog.HandlerOptions{Level: …}` in `main`. Gotcha: don't log the header value on *failed* auth (it's attacker-controlled noise) — log only `remote` there; and keep the user out of any error written to the client.

**T-5 (bridge tests).** `pingLoop` takes `(ctx, cancel, conn)` — it's already testable if `conn` is narrowed: introduce a one-method `pinger interface { Ping(context.Context) error }` (mirroring the existing `wsConn` pattern at `session.go:21-24`), inject a fake whose Ping fails after N calls, set `pingInterval` to a few ms, and assert the shared context is cancelled. For `Attach`, the executor construction is the hard edge — factor `NewExecutor` behind a function field on `KubeAttacher` defaulting to the real one, so a test can substitute a fake executor and assert session wiring + context teardown. Gotcha: keep the production zero-value path identical (default field in `NewKubeAttacher`) so this stays a test seam, not an API change.

---

## Open Questions (need a human decision)

1. **Is `fr.run/id` a protocol constant or configuration?** (drives T-8) The README sells `LABEL_SELECTOR` as portability, but `Resolve` and the tmux-session convention hardwire `fr.run/id`. Freeze it or thread it through?
2. **Will runs-fr ever be multi-user?** If yes, the SEC-2 allowlist hook and SEC-5 session caps move up a milestone; if it stays single-operator-behind-Authentik, the audit log alone is the right scope.
3. **Is the SPDY fallback still needed for the target clusters?** Talos/recent k8s all speak the websocket exec protocol; dropping `NewSPDYExecutor` (`internal/term/exec.go:50`) would remove the `moby/spdystream` dependency (one current CVE source) entirely.
4. **When does Phase 8 (issue #9) run?** T-6 proposes converting it to a scripted check — confirm whether the manual cluster pass should still happen first against frank.
5. **Performance/scale targets:** is one replica with 128Mi the intended ceiling? Replica scaling is claimed safe (`values.yaml:16-18`) but has never been exercised.

---

*Methodology note: findings grounded in the checkout at `ec6b28a`; `go vet`, `go test ./...` (pass), per-package coverage, and `govulncheck` (31 reachable findings) were executed on 2026-06-10 in the audit worktree. The `-race` suite could not run in the audit sandbox (no cgo) but is enforced in CI (`ci.yml:33-34`).*
