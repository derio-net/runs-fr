<!-- fr:journal-schema=4 -->
# Journal: 2026-11-15-runs-fr-deps-gate

<!-- fr:journal kind=discovery scope=spec id=operator-brief created=2026-11-15T07:03:15+00:00 input=true -->
### operator-brief · discovery · Operator brief (verbatim) — batch runs-fr-deps-gate

/fr-goal runs-fr: supported Go toolchain, govulncheck in CI, dependency bot

Batch `runs-fr-deps-gate` of derio-net/runs-fr: 3 issues, delivered as ONE pull request.

## runs-fr#31: [High] Bump EOL Go toolchain + stale deps to clear 31 reachable govulncheck vulns
Still EOL: `go.mod` `go 1.23.0 / toolchain go1.23.4`, `Dockerfile:25` `golang:1.23-alpine`, `ci.yml:19` go-version 1.23. Reachable stdlib/x-net vulns on a shell gateway.
Note: Batch with runs-fr#32, #37 (deps + gates).

## runs-fr#32: [High] Add govulncheck to CI (report-only, then hard gate)
No `govulncheck` anywhere in `.github/` on main.

## runs-fr#37: [Medium] Add Renovate/Dependabot (gomod, npm, Dockerfile, Actions)
No renovate.json or dependabot config tracked on main.

## Why these belong together
The shell gateway is on an end-of-life Go with reachable vulnerabilities and has no vulnerability check or dependency updates; one go.mod and one ci.yml.

## Delivery rules
- Work on branch `feat/batch-runs-fr-deps-gate`.
- Open a draft PR as soon as the spec is committed. Its body contains these lines, one per member, so every member closes when it merges:
  Closes derio-net/runs-fr#31
  Closes derio-net/runs-fr#32
  Closes derio-net/runs-fr#37
- Do not name any member issue as a phase `tracking_issue` in the plan: the bridge would then own that issue's `fr:` labels.

Never add the `approved-fr` label: it is the operator's approval to merge, and fr voids it on your next push.

<!-- fr:journal kind=decision scope=spec id=go-target created=2026-11-15T07:03:15+00:00 decided_by=operator -->
### go-target · decision · Go 1.27 everywhere; CI reads the version from go.mod

Operator chose (recommended): go.mod `go 1.27.0` + `toolchain go1.27.4`, Dockerfile `golang:1.27-alpine`, ci.yml `go-version-file: go.mod`. Rejected: 1.26 floor with 1.27 toolchain; 1.26 everywhere. (R1)

<!-- fr:journal kind=decision scope=spec id=k8s-ladder created=2026-11-15T07:03:15+00:00 decided_by=operator -->
### k8s-ladder · decision · Ladder k8s.io/* one minor per commit to latest v0.37.x

Operator chose (recommended): 0.32→0.33→…→0.37, vet + test -race green per rung, one commit per rung. Rejected: jump straight to 0.37; stop at 0.36. (R2)

<!-- fr:journal kind=decision scope=spec id=vuln-gate-mode created=2026-11-15T07:03:15+00:00 decided_by=operator -->
### vuln-gate-mode · decision · govulncheck ships as a hard gate plus a weekly scheduled scan

Operator chose (recommended): fail CI on any reachable vuln from day one, plus a weekly schedule on main. Rejected: hard gate on PR/push only; report-only now with a follow-up flip. #32's report-only step is moot because the bump lands in the same PR. (R5)

<!-- fr:journal kind=decision scope=spec id=dep-bot created=2026-11-15T07:03:15+00:00 decided_by=operator -->
### dep-bot · decision · Renovate, grouped, automerge off

Operator chose (recommended): renovate.json, weekly, dependency dashboard, gomodTidy, groups for k8s.io, golang.org/x, xterm, Actions, Go toolchain; automerge off, matching frank. Rejected: Dependabot; Renovate + Dependabot security updates. App enablement for the repo, if repo-selected, is an operator step. (R8)

<!-- fr:journal kind=decision scope=spec id=bump-scope created=2026-11-15T07:03:15+00:00 decided_by=operator -->
### bump-scope · decision · Bump Go + only what Go forces (golangci-lint); the rest to Renovate

Operator chose (recommended): Go toolchain, Go modules and golangci-lint by hand; Node, action majors, esbuild, xterm, e2e pip pins left for Renovate's first PRs. Rejected: everything stale now; Go + Actions alignment. (R7, R10)

<!-- fr:journal kind=decision scope=spec id=verification-gate created=2026-11-15T07:03:15+00:00 decided_by=operator -->
### verification-gate · decision · Attach row is live; run gate tester

Operator chose (recommended): PR CI verifies the build, vuln and gate rows pre-merge; a post-merge Test Plan covers the staging-gate e2e, an operator attach check and Renovate's first activity; `gate: tester` holds the close-out until the walk is recorded. Rejected: gate developer; CI only with no live row. No `.fr/candidate-install` exists, so `candidate` was not offered.

<!-- fr:journal kind=finding scope=spec id=s-r1-1 created=2026-11-15T07:09:23+00:00 state=open review_scope=in -->
### s-r1-1 · finding [open] (reviewer: in scope) · deps-gate-supported-toolchain claims PR CI verifies the Docker build stage, but no PR job builds the image

Evidence: ci.yml:13-43 has no docker build; build.yml:7-9 builds only on push to main; Dockerfile:25 is changed by this spec. The golang:1.27-alpine stage would first compile after merge.

<!-- fr:journal kind=finding scope=spec id=s-r1-2 created=2026-11-15T07:09:23+00:00 state=open review_scope=in -->
### s-r1-2 · finding [open] (reviewer: in scope) · Weekly-scan half of deps-gate-vuln-gate-blocks is provable only post-merge, yet the row was verify: none, the Test Plan never checked it, and 'surfaces' had no mechanism

Evidence: the row's text; spec §Verification; Test Plan items 1-3. A schedule fires only from the default branch; a failed scheduled run notifies only the last cron editor; GitHub disables schedules after 60 days of inactivity.

<!-- fr:journal kind=finding scope=spec id=s-r1-3 created=2026-11-15T07:09:23+00:00 state=open review_scope=in -->
### s-r1-3 · finding [open] (reviewer: in scope) · deps-gate-vuln-gate-blocks cited R6 (per-package coverage), which its text does not cover

Evidence: docs/acceptance/features/2026-11-15-runs-fr-deps-gate.yaml:49-51 (origin R5, R6); spec R6 was the -cover requirement.

<!-- fr:journal kind=finding scope=spec id=s-r1-4 created=2026-11-15T07:09:23+00:00 state=open review_scope=in -->
### s-r1-4 · finding [open] (reviewer: in scope) · Renovate does not track the golangci-lint pin; a Renovate Go-minor bump would break lint

Evidence: ci.yml:40-43 (`version: v2.3` is an action input, not a `uses:` ref); the spec's customManagers covered only pip and govulncheck.

<!-- fr:journal kind=finding scope=spec id=s-r1-5 created=2026-11-15T07:09:23+00:00 state=open review_scope=in -->
### s-r1-5 · finding [open] (reviewer: in scope) · The 'go toolchain' group's claim to keep go.mod and the golang image in one PR is not established

Evidence: spec §Renovate group (matchDatasources golang-version/docker); go.mod:3,5; Dockerfile:25. Renovate may treat the `go` directive as a floor, and toolchain patch bumps have no matching minor-only image tag change.

<!-- fr:journal kind=finding scope=spec id=s-r1-6 created=2026-11-15T07:09:23+00:00 state=open review_scope=in -->
### s-r1-6 · finding [open] (reviewer: in scope) · R3's 'go mod tidy leaves go.mod/go.sum clean' had no check in CI

Evidence: spec R3; the row deps-gate-no-reachable-vulns cites R3 but was verified only by govulncheck; ci.yml:13-43 has no tidy check.

<!-- fr:journal kind=finding scope=spec id=s-r1-7 created=2026-11-15T07:09:23+00:00 state=open review_scope=in -->
### s-r1-7 · finding [open] (reviewer: in scope) · Design added a workflow_dispatch trigger that R5 did not mention

Evidence: spec §Vulnerability gate (`workflow_dispatch:`) vs R5 (PR, push, schedule only).

<!-- fr:journal kind=finding scope=spec id=s-r1-8 created=2026-11-15T07:09:23+00:00 state=open review_scope=in -->
### s-r1-8 · finding [open] (reviewer: in scope) · Makefile `test` target diverges from CI's test step once -cover is added

Evidence: Makefile:7-8 (`go test ./... -race`); README documents make targets as CI's local mirror. Reviewer tagged this out of scope; reclassified in because this change introduces the -cover divergence, so it is this change's to keep in step.

<!-- fr:journal kind=review scope=spec id=spec-review-r1 created=2026-11-15T07:09:23+00:00 -->
### spec-review-r1 · review · Independent spec review r1: 8 findings (reviewer: 7 in, 1 out; s-r1-8 reclassified in); all fixed in the spec

Dispatched fr-spec-reviewer (read-only, in this workspace) against the spec, the spec journal's 6 operator decisions, and the codebase. It confirmed all 6 decisions are honoured, that no other Go version pin exists (release.yml/build.yml have no setup-go; the devcontainer uses the unpinned go:1 feature), that the govulncheck job compiles without a frontend build (tracked static/index.html and xterm.css satisfy the embed glob), that schedule/workflow_dispatch do not interact with the CI-authority `if:`, and that the Renovate manager names, managerFilePatterns syntax and the pip/govulncheck regexes are correct.
Findings s-r1-1..s-r1-8 raised; all fixed in the spec (see resolves).

<!-- fr:journal kind=finding scope=spec id=s-r1-1-resolved created=2026-11-15T07:09:23+00:00 state=fixed resolves=s-r1-1 -->
### s-r1-1-resolved · finding [fixed] · resolves s-r1-1: deps-gate-supported-toolchain claims PR CI verifies the Docker build stage, but no PR job builds the image

New R9 wording plus a new §'Image build on PRs': ci.yml gains an `image` job (buildx + build-push-action@v6, push: false, same majors as build.yml). The row's Verification line now names the `image` job.

<!-- fr:journal kind=finding scope=spec id=s-r1-2-resolved created=2026-11-15T07:09:23+00:00 state=fixed resolves=s-r1-2 -->
### s-r1-2-resolved · finding [fixed] · resolves s-r1-2: Weekly-scan half of deps-gate-vuln-gate-blocks is provable only post-merge, yet the row was verify: none, the Test Plan never checked it, and 'surfaces' had no mechanism

R6 is now the weekly schedule + workflow_dispatch + surfacing requirement: a scheduled govulncheck failure on GitHub opens or comments on a `govulncheck: reachable vulnerability on main` issue (job-scoped issues: write). The 60-day schedule-disable limit is documented. The row is verify: live (fr acceptance set-status, baf0e37) and Test Plan item 3 exercises workflow_dispatch and checks for a scheduled run.

<!-- fr:journal kind=finding scope=spec id=s-r1-3-resolved created=2026-11-15T07:09:23+00:00 state=fixed resolves=s-r1-3 -->
### s-r1-3-resolved · finding [fixed] · resolves s-r1-3: deps-gate-vuln-gate-blocks cited R6 (per-package coverage), which its text does not cover

Requirements renumbered so the row's existing citations are true: R5 = the PR/push hard gate, R6 = the weekly scan and its surfacing, which is what the row states. Coverage moved to R11, which no row cites (an implementation detail of #32's baseline, not a business-level claim).

<!-- fr:journal kind=finding scope=spec id=s-r1-4-resolved created=2026-11-15T07:09:23+00:00 state=fixed resolves=s-r1-4 -->
### s-r1-4-resolved · finding [fixed] · resolves s-r1-4: Renovate does not track the golangci-lint pin; a Renovate Go-minor bump would break lint

R8 and the Renovate design add a regex customManager for golangci-lint-action's `version:` input (github-releases, depName golangci/golangci-lint), placed in the `go toolchain` group so a new Go minor lands together with a linter that supports it.

<!-- fr:journal kind=finding scope=spec id=s-r1-5-resolved created=2026-11-15T07:09:23+00:00 state=fixed resolves=s-r1-5 -->
### s-r1-5-resolved · finding [fixed] · resolves s-r1-5: The 'go toolchain' group's claim to keep go.mod and the golang image in one PR is not established

Added a 'Drift policy' paragraph: the invariant is the Go minor. The `go` directive gets `rangeStrategy: bump` (matchDepTypes golang); the group matches by package name (go, golang, golangci/golangci-lint) across datasources; toolchain-only patch bumps are consistent with the minor-only image tag. The plan validates with renovate-config-validator AND a `--dry-run=extract` that must list deps from every manager.

<!-- fr:journal kind=finding scope=spec id=s-r1-6-resolved created=2026-11-15T07:09:23+00:00 state=fixed resolves=s-r1-6 -->
### s-r1-6-resolved · finding [fixed] · resolves s-r1-6: R3's 'go mod tidy leaves go.mod/go.sum clean' had no check in CI

R3 now requires CI to fail on drift; the design adds a `go mod tidy -diff` step to the `go` job; the row's Verification line names it.

<!-- fr:journal kind=finding scope=spec id=s-r1-7-resolved created=2026-11-15T07:09:23+00:00 state=fixed resolves=s-r1-7 -->
### s-r1-7-resolved · finding [fixed] · resolves s-r1-7: Design added a workflow_dispatch trigger that R5 did not mention

workflow_dispatch is now in R6 with its purpose (exercise the scheduled path on demand), and Test Plan item 3 uses it.

<!-- fr:journal kind=finding scope=spec id=s-r1-8-resolved created=2026-11-15T07:09:23+00:00 state=fixed resolves=s-r1-8 -->
### s-r1-8-resolved · finding [fixed] · resolves s-r1-8: Makefile `test` target diverges from CI's test step once -cover is added

R11 now covers both the CI test step and `make test`; the design adds -cover to the Makefile target.

<!-- fr:journal kind=decision scope=spec id=exit-baseline created=2026-11-15T07:09:25+00:00 -->
### exit-baseline · decision · exit when spec-review resolved

The exit when spec-review resolved; `fr verification set` and `deliver` compare against it.

```yaml
row_gates:
  deps-gate-attach-works: tester
  deps-gate-dependency-bot: tester
  deps-gate-no-reachable-vulns: none
  deps-gate-supported-toolchain: none
  deps-gate-vuln-gate-blocks: tester
row_kinds:
  deps-gate-attach-works: live
  deps-gate-dependency-bot: live
  deps-gate-no-reachable-vulns: none
  deps-gate-supported-toolchain: none
  deps-gate-vuln-gate-blocks: live
run_gate: tester
run_kind: none
```
