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
