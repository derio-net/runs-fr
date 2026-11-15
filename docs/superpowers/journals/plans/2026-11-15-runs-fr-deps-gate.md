<!-- fr:journal-schema=4 -->
# Journal: 2026-11-15-runs-fr-deps-gate

<!-- fr:journal kind=lens scope=plan id=lens-security-1 created=2026-11-15T07:12:01+00:00 lens=security step=plan -->
### lens-security-1 · lens · Shell gateway on a new Go/client-go, plus a CI job granted issues: write and a scheduled trigger — review the whole diff for privilege and supply-chain exposure

<!-- fr:journal kind=discovery scope=plan id=p1-govulncheck-prebump created=2026-11-15T07:59:28+00:00 phase=1 -->
### p1-govulncheck-prebump · discovery · govulncheck pin v1.9.0; the pre-bump tree exits 3 with 50 reachable vulnerabilities (phase 1)

Pin: golang.org/x/vuln v1.9.0 (newest non-prerelease). It requires go >= 1.26, so the
plan's literal `GOTOOLCHAIN=go1.23.4 go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`
refuses to build. Instead the v1.9.0 binary was built with Go 1.27.4
(`GOBIN=$PWD/bin go install …@v1.9.0`) and run as `GOTOOLCHAIN=go1.23.4 ./bin/govulncheck ./...`,
so it loaded the packages with the go1.23.4 toolchain and stdlib (findings read
"Found in: net/http@go1.23.4").

Result on the unmodified tree: exit 3, "Your code is affected by 50 vulnerabilities from
3 modules and the Go standard library." Modules named: golang.org/x/net (v0.30.0),
github.com/moby/spdystream (v0.5.0), golang.org/x/text, github.com/gorilla/websocket;
the bulk is stdlib (net/http, crypto/x509, crypto/tls). This is the evidence that the
gate fails on a reachable vulnerability (row deps-gate-vuln-gate-blocks, PR/push half).

After the toolchain and k8s ladder: 5 vulnerabilities, all golang.org/x/net v0.57.0
(fixed in v0.60.0). After `go get golang.org/x/net@latest` (v0.62.0, pulling x/sys
v0.49.0, x/term v0.47.0, x/text v0.43.0): "No vulnerabilities found.", exit 0.
spdystream was already moved to v0.5.1 (its latest) by the ladder.

Note for CI: through `go run`, a govulncheck exit 3 surfaces as exit 1 (go run maps a
non-zero child exit to 1); the job still fails, which is all the gate needs.

<!-- fr:journal kind=discovery scope=plan id=p1-k8s-ladder-spdy-fallback created=2026-11-15T07:59:28+00:00 phase=1 -->
### p1-k8s-ladder-spdy-fallback · discovery · k8s ladder v0.32.13 → v0.33.13 → v0.34.13 → v0.35.10 → v0.36.6 → v0.37.2: no API break, but the SPDY fallback silently stopped firing from v0.36 (phase 1)

Every rung was green on go vet and go test -race with no code change in internal/term.
golangci-lint v2.15.0 then flagged (SA1019) that k8s.io/apimachinery/pkg/util/httpstream
is deprecated in favour of k8s.io/streaming/pkg/httpstream. Tracing it showed a real
behavioural regression: since client-go v0.36 the WebSocket round-tripper returns
k8s.io/streaming's UpgradeFailureError, a distinct struct from apimachinery's copy, so
`httpstream.IsUpgradeFailure` (apimachinery) passed to `remotecommand.NewFallbackExecutor`
never matched and a refused WebSocket upgrade no longer fell back to SPDY. The suite
did not catch it because nothing exercised the fallback.

Fix (commit 851b2fa, separate from the rung commits because no rung broke the build or a
test): internal/term/exec.go now uses k8s.io/streaming/pkg/httpstream.IsUpgradeFailure
(k8s.io/streaming v0.37.2 becomes a direct requirement), still via
`remotecommand.NewFallbackExecutor(ws, spdy, httpstream.IsUpgradeFailure)`. New test
internal/term/fallback_test.go drives the real client-go transports against an httptest
server that refuses every upgrade and asserts a POST (SPDY) follows the GET (WebSocket);
it failed with methods=[GET] before the fix and passes after. This matters to the live
row deps-gate-attach-works: an older API server or an Upgrade-stripping proxy would have
broken attach after merge.

<!-- fr:journal kind=discovery scope=plan id=p1-golangci-lint-pin created=2026-11-15T07:59:28+00:00 phase=1 -->
### p1-golangci-lint-pin · discovery · golangci-lint pinned to v2.15.0 (Go 1.27 support since v2.13.0) (phase 1)

The changelog lists "go1.27 support" under v2.13.0; v2.15.0 is the newest v2.x release
and was picked. Locally (`go run …/golangci-lint@v2.15.0 run`) it reported 3 findings,
all fixed in code, no linter disabled: an unchecked receive in
internal/localterm/attacher_test.go, an unchecked Close in test/pins/pins_test.go, and
the SA1019 httpstream deprecation (see p1-k8s-ladder-spdy-fallback). 0 issues after.

<!-- fr:journal kind=discovery scope=plan id=p1-ci-authority-if-repeated created=2026-11-15T07:59:28+00:00 phase=1 -->
### p1-ci-authority-if-repeated · discovery · Authority if: stays repeated per job in ci.yml (phase 1)

P1.T6.S4: GitHub Actions has accepted YAML anchors since 2025, but this workflow also
runs on the Gitea Actions mirror, whose support for anchors is not established, and
deduping would change nothing at runtime. The expression is repeated verbatim on go,
govulncheck, image and helm, and TestCIGates asserts the new jobs and helm carry the go
job's exact `if:`. ci.yml comments explain why govulncheck needs no Node step and why
issues: write is job-scoped.

<!-- fr:journal kind=discovery scope=plan id=p1-acceptance-ref-grammar created=2026-11-15T07:59:28+00:00 phase=1 -->
### p1-acceptance-ref-grammar · discovery · Acceptance levels: keys are unit/api/int/ui and refs are '<repo>:<path>', so the plan's T7.S2 command needed adapting (phase 1)

`fr acceptance set-status` refuses the plan's `--level ci=derio-net/runs-fr:…`: the level
keys are unit | api | int | ui (no `ci`), and the ref's repo part may not contain "/"
(it is `runs-fr:<path>`). Rows moved to `ci` with: deps-gate-supported-toolchain —
unit=runs-fr:.github/workflows/ci.yml#go, unit=runs-fr:test/pins/pins_test.go#TestGoVersionPinsAgree,
int=runs-fr:.github/workflows/ci.yml#image; deps-gate-no-reachable-vulns —
int=runs-fr:.github/workflows/ci.yml#govulncheck, int=runs-fr:.github/workflows/ci.yml#go
(the tidy step). set-status also refuses `--level` without `--status`, so
deps-gate-vuln-gate-blocks and deps-gate-attach-works were left untouched at
not-implemented / verify: live; their candidate unit levels are
runs-fr:test/pins/ci_gates_test.go#TestCIGates and
runs-fr:internal/term/fallback_test.go#TestNewExecutorFallsBackToSPDYOnUpgradeFailure.

<!-- fr:journal kind=discovery scope=plan id=no-refactor-p1-t1 created=2026-11-15T07:59:28+00:00 phase=1 -->
### no-refactor-p1-t1 · discovery · no-refactor-because P1.T1 (phase 1)

Measurement only (baseline suite and a captured govulncheck run); no code was written.

<!-- fr:journal kind=discovery scope=plan id=no-refactor-p1-t3 created=2026-11-15T07:59:28+00:00 phase=1 -->
### no-refactor-p1-t3 · discovery · no-refactor-because P1.T3 (phase 1)

Version-only rungs (go.mod/go.sum); the existing suite is the guard. No rung broke an API in internal/term, so there was no adapted code to clean. The behavioural break found across the ladder was fixed and tested in its own commit (851b2fa).

<!-- fr:journal kind=discovery scope=plan id=no-refactor-p1-t4 created=2026-11-15T07:59:28+00:00 phase=1 -->
### no-refactor-p1-t4 · discovery · no-refactor-because P1.T4 (phase 1)

go get/go mod tidy of indirect modules only; no code changed.

<!-- fr:journal kind=discovery scope=plan id=no-refactor-p1-t5 created=2026-11-15T07:59:28+00:00 phase=1 -->
### no-refactor-p1-t5 · discovery · no-refactor-because P1.T5 (phase 1)

Two one-line lint fixes in tests plus the CI version pin; nothing left to restructure.

<!-- fr:journal kind=discovery scope=plan id=no-refactor-p1-t7 created=2026-11-15T07:59:28+00:00 phase=1 -->
### no-refactor-p1-t7 · discovery · no-refactor-because P1.T7 (phase 1)

Push, CI read and acceptance-row moves via fr; no code written.

<!-- fr:journal kind=finding scope=plan id=p1-r1 created=2026-11-15T08:11:22+00:00 phase=1 state=open review_scope=in -->
### p1-r1 · finding [open] (reviewer: in scope) · pins test fails when go.mod has no toolchain line, which cmd/go/tidy produces when toolchain equals the go directive (phase 1)

test/pins/pins_test.go:89-92 made a missing toolchain line fatal; cmd/go drops a toolchain line equal to the go directive, which phase-2 Renovate (rangeStrategy bump + gomodTidy) makes likely.

<!-- fr:journal kind=finding scope=plan id=p1-r2 created=2026-11-15T08:11:22+00:00 phase=1 state=open review_scope=in -->
### p1-r2 · finding [open] (reviewer: in scope) · issue step fired on any job failure (and on govulncheck errors, exit 1) but always titled it 'reachable vulnerability' (phase 1)

ci.yml:84 `if: failure()` covered checkout/setup-go/vuln-DB failures too.

<!-- fr:journal kind=finding scope=plan id=p1-r3 created=2026-11-15T08:11:22+00:00 phase=1 state=open review_scope=in -->
### p1-r3 · finding [open] (reviewer: in scope) · issues: write sat on the scan job (with persisted checkout creds) for every PR/push run (phase 1)

ci.yml:58-66; only the scheduled failure path needs a write scope.

<!-- fr:journal kind=finding scope=plan id=p1-r4 created=2026-11-15T08:11:22+00:00 phase=1 state=open review_scope=in -->
### p1-r4 · finding [open] (reviewer: in scope) · setup-go@v5 installs 1.27.0 then auto-downloads go1.27.4, colliding with the cache restore; spec claimed setup-go honours the toolchain line (phase 1)

ci.yml:27-30,68-71; run 43692080590 logged ~24k tar 'File exists' lines. Correct result (go1.27.4 runs), false spec claim, noisy logs. setup-go v6 is barred by R10.

<!-- fr:journal kind=finding scope=plan id=p1-r5 created=2026-11-15T08:11:22+00:00 phase=1 state=open review_scope=in -->
### p1-r5 · finding [open] (reviewer: in scope) · k8s v0.36/v0.37 rung commits ship the broken SPDY fallback; the fix lands two commits later, contrary to R2 (phase 1)

7d78940 and 4144a8c pass tests only because fallback_test.go did not exist yet; 851b2fa fixes it.

<!-- fr:journal kind=finding scope=plan id=p1-r6 created=2026-11-15T08:11:22+00:00 phase=1 state=open review_scope=in -->
### p1-r6 · finding [open] (reviewer: in scope) · Dockerfile build-stage regex rejected a digest-pinned golang image with a misleading message (phase 1)

test/pins/pins_test.go:79 reBuildStage had no `@sha256:` allowance.

<!-- fr:journal kind=finding scope=plan id=p1-r7 created=2026-11-15T08:11:22+00:00 phase=1 state=open review_scope=in -->
### p1-r7 · finding [open] (reviewer: in scope) · attacher_test receive change looked like churn rather than a lint finding (phase 1)

internal/localterm/attacher_test.go:100 `case <-done:` → `case err := <-done:`.

<!-- fr:journal kind=finding scope=plan id=p1-o1 created=2026-11-15T08:11:22+00:00 phase=1 state=open review_scope=in -->
### p1-o1 · finding [open] (reviewer: in scope) · Scaffolded acceptance-report matrix job lacks the CI-authority if:, so it also runs (and fails) on the Gitea mirror (phase 1)

Found by the orchestrator on PR #46's checks (gitea-actions/acceptance-report / matrix: fail). .github/workflows/acceptance-report.yml:33 had no `if:`, unlike every ci.yml job; added in this PR by `fr acceptance init`.

<!-- fr:journal kind=discovery scope=plan id=p1-bisect-note created=2026-11-15T08:11:22+00:00 phase=1 -->
### p1-bisect-note · discovery · Bisect note: commits 7d78940 (k8s v0.36.6) and 4144a8c (v0.37.2) carry a silently broken SPDY exec fallback, fixed in 851b2fa (phase 1)

Any bisect of an attach regression that lands on those two commits should treat 851b2fa as part of them. Not folded: the commits were already pushed, and this repo's git.history is `keep` (fr push never rewrites pushed history).

<!-- fr:journal kind=lens scope=plan id=lens-tests-1 created=2026-11-15T08:11:22+00:00 phase=1 lens=tests step=review-phase -->
### lens-tests-1 · lens · The pins and CI-gate parser tests must accept every file shape Renovate (with gomodTidy, digest pinning) actually writes — reviewer-suggested renovate-compat concern (phase 1)

<!-- fr:journal kind=review scope=plan id=p1-review-r1 created=2026-11-15T08:11:22+00:00 phase=1 -->
### p1-review-r1 · review · Phase 1 review r1: 7 findings (1 important, 6 minor, all tagged in) plus 1 orchestrator finding; 6 fixed, 2 refuted (phase 1)

Dispatched an independent read-only reviewer over 35fec9f..65a8a22 (code paths only), against spec R1–R7, R9, R11 and plan phase 1. It re-ran tidy -diff, vet, test -race -cover and govulncheck at HEAD (all clean), confirmed PR CI green, and confirmed the SPDY fallback fix and its test are a real regression guard. Raised p1-r1..p1-r7; the orchestrator added p1-o1 from PR #46's checks.
Received per superpowers:receiving-code-review, each verified against the code:
- p1-r1, p1-r6 fixed test-first (TestParseGoMod, TestParseBuildStage red, then pure parsers green) in 3194bda.
- p1-r2, p1-r3 fixed test-first (TestCIGates extended red, then ci.yml green) in 4549f11; also found that `go run` masks govulncheck's exit 3, so the scan now runs an installed binary.
- p1-r4: spec claim corrected and the behaviour commented in ci.yml; the v6 bump stays Renovate's (R10).
- p1-o1 fixed in b2f6b34.
- p1-r5, p1-r7 refuted with evidence (see resolves).

<!-- fr:journal kind=finding scope=plan id=p1-r1-resolved created=2026-11-15T08:11:22+00:00 phase=1 state=fixed resolves=p1-r1 -->
### p1-r1-resolved · finding [fixed] · resolves p1-r1: pins test fails when go.mod has no toolchain line, which cmd/go/tidy produces when toolchain equals the go directive (phase 1)

3194bda: parseGoMod treats a missing toolchain line as equal to the go directive; TestParseGoMod covers go+toolchain, toolchain omitted, and no go directive.

<!-- fr:journal kind=finding scope=plan id=p1-r2-resolved created=2026-11-15T08:11:22+00:00 phase=1 state=fixed resolves=p1-r2 -->
### p1-r2-resolved · finding [fixed] · resolves p1-r2: issue step fired on any job failure (and on govulncheck errors, exit 1) but always titled it 'reachable vulnerability' (phase 1)

4549f11: the scan publishes govulncheck's exit code; govulncheck-issue titles exit 3 'reachable vulnerability on main' and anything else 'scheduled scan failed on main'. TestCIGates asserts both titles and the outputs wiring. The reviewer's fix would not have worked as written: `go run` turns govulncheck's exit 3 into 1, so the scan now installs and runs the binary (TestCIGates forbids `go run` there).

<!-- fr:journal kind=finding scope=plan id=p1-r3-resolved created=2026-11-15T08:11:22+00:00 phase=1 state=fixed resolves=p1-r3 -->
### p1-r3-resolved · finding [fixed] · resolves p1-r3: issues: write sat on the scan job (with persisted checkout creds) for every PR/push run (phase 1)

4549f11: issue filing moved to a schedule-only govulncheck-issue job (needs: govulncheck, failure() && schedule && github.com, permissions: issues: write, no checkout); the scan job is contents: read with persist-credentials: false. TestCIGates asserts each property with a line-anchored permissions match.

<!-- fr:journal kind=finding scope=plan id=p1-r4-resolved created=2026-11-15T08:11:22+00:00 phase=1 state=fixed resolves=p1-r4 -->
### p1-r4-resolved · finding [fixed] · resolves p1-r4: setup-go@v5 installs 1.27.0 then auto-downloads go1.27.4, colliding with the cache restore; spec claimed setup-go honours the toolchain line (phase 1)

8fa857c corrects the spec's false 'setup-go honours the toolchain line' claim and states v5's actual behaviour (go then fetches go1.27.4 via GOTOOLCHAIN=auto, so commands run on 1.27.4); 4549f11 comments it in ci.yml. The remaining log noise is accepted: removing it needs setup-go v6, an action-major bump R10 leaves to Renovate's first github-actions PR, and GOTOOLCHAIN=local would test on 1.27.0.

<!-- fr:journal kind=finding scope=plan id=p1-r5-resolved created=2026-11-15T08:11:22+00:00 phase=1 state=refuted resolves=p1-r5 -->
### p1-r5-resolved · finding [refuted] · resolves p1-r5: k8s v0.36/v0.37 rung commits ship the broken SPDY fallback; the fix lands two commits later, contrary to R2 (phase 1)

Real, but not fixable in this PR: folding 851b2fa into 7d78940 rewrites already-pushed commits, and this repo's git.history is `keep` (fr push never rewrites or forces). Mitigated instead by the discovery p1-bisect-note, carried into the PR body, naming both commits and the fix.

<!-- fr:journal kind=finding scope=plan id=p1-r6-resolved created=2026-11-15T08:11:22+00:00 phase=1 state=fixed resolves=p1-r6 -->
### p1-r6-resolved · finding [fixed] · resolves p1-r6: Dockerfile build-stage regex rejected a digest-pinned golang image with a misleading message (phase 1)

3194bda: reBuildStage accepts an optional @sha256:<hex> digest and the error names the digest form; TestParseBuildStage covers minor tag, patch tag, digest-pinned and a non-build stage.

<!-- fr:journal kind=finding scope=plan id=p1-r7-resolved created=2026-11-15T08:11:22+00:00 phase=1 state=refuted resolves=p1-r7 -->
### p1-r7-resolved · finding [refuted] · resolves p1-r7: attacher_test receive change looked like churn rather than a lint finding (phase 1)

Verified by reverting the line and running golangci-lint v2.15.0: `internal/localterm/attacher_test.go:100:7: Error return value is not checked (errcheck)` on `case <-done:`. It is a real errcheck finding, and the change is its fix.

<!-- fr:journal kind=finding scope=plan id=p1-o1-resolved created=2026-11-15T08:11:22+00:00 phase=1 state=fixed resolves=p1-o1 -->
### p1-o1-resolved · finding [fixed] · resolves p1-o1: Scaffolded acceptance-report matrix job lacks the CI-authority if:, so it also runs (and fails) on the Gitea mirror (phase 1)

b2f6b34 adds the CI-authority if: to the acceptance-report matrix job (pages, which needs it, skips with it on Gitea).
