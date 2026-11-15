# runs-fr deps gate — implementation plan

Spec: `docs/superpowers/specs/2026-11-15-runs-fr-deps-gate-design.md`.
Batch `runs-fr-deps-gate` (runs-fr#31, #32, #37), one PR. No phase tracks a
member issue: the PR body's `Closes` lines own them.

## Shape

- **Phase 1 — toolchain + gate (#31, #32), skeleton, `hard`.** The two asks are
  coupled: the gate can only ship hard once the bump clears every reachable
  vuln, and both edit `ci.yml`. Its first task is the walking-skeleton smoke:
  the existing suite green on the untouched tree, and a *captured* pre-bump
  govulncheck run (old toolchain, exit 3) that doubles as the evidence the gate
  fails on a reachable vuln. Then: Go 1.27 behind a new `test/pins` test that
  makes "go.mod, Dockerfile and CI agree" a failing test rather than a promise;
  the k8s ladder one minor per commit; the remaining module bumps until
  govulncheck is clean; golangci-lint for 1.27; and the CI jobs, again
  test-first (`TestCIGates`), validated with actionlint and proven on the PR.
- **Phase 2 — Renovate (#37), `standard`.** Config written to the spec, then
  proven two ways: the strict schema validator, and a local
  `--dry-run=extract` that must find deps for every manager including the three
  regex managers. Depends on phase 1 so its regexes match the final `ci.yml`.
- **Phase 3 — `[manual]`, back-loaded.** Give the org's Renovate installation
  access to runs-fr, if it is repo-selected. Nothing agentic depends on it; the
  PR ships it unimplemented and the operator does it in the GitHub UI.

## Verification

PR CI is the pre-merge evidence (`tests: ci` is acceptable for phase 1, whose
last task pushes and reads the checks). The post-merge rows —
`deps-gate-vuln-gate-blocks` (weekly half), `deps-gate-attach-works`,
`deps-gate-dependency-bot` — are the spec's Test Plan, under `gate: tester`.

## Risks

- client-go minors can change `remotecommand` behaviour; the ladder isolates
  each minor in its own commit and its own green suite.
- golangci-lint must be new enough to parse Go 1.27; the phase picks the
  version from release notes, not by guess.
- Renovate's handling of the `go` directive is configured explicitly
  (`rangeStrategy: bump`) and verified by the extraction dry run.
