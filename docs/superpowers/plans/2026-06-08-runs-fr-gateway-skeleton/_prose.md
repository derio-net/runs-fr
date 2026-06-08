# runs-fr gateway — skeleton

## What this builds

`runs-fr` is a Kubernetes-native, **portable** web shell gateway: it lists live
agent-run pods (by an `fr.run/*` label selector) and lets you attach to a pod's
named session in the browser, bridging the Kubernetes `pods/exec` subresource to
an xterm.js terminal. It is the **Component C** slice of the k8s-native agentic
runs design (spec: `derio-net/frank:docs/superpowers/specs/2026-06-07--agents--k8s-native-runs-design.md`).

This plan is the **walking-skeleton** of that gateway, scoped to the `runs-fr`
repo only. It is deliberately cluster-agnostic: namespace, label selector, and
the trusted auth header are configuration, so the same image/chart installs into
any cluster.

## Stack (the approved hybrid)

- **Server (Go):** own the ~300-line exec↔ws bridge rather than vendor a
  terminal app — minimal dependency surface for a long-lived product. Built on
  `k8s.io/client-go` (`remotecommand` + `kubernetes`) and
  `github.com/coder/websocket`. Exec uses
  `remotecommand.NewFallbackExecutor(WebSocket, SPDY)` for cluster-version
  robustness. The `terminalSession` type adapts the dashboard's `TerminalSession`
  pattern as a *reference* (not a dependency).
- **Frontend:** `@xterm/xterm` + `@xterm/addon-fit` + `@xterm/addon-attach`
  (the attach addon removes the hand-written stdin loop), bundled with esbuild
  and embedded into the binary via `embed.FS`.
- **Packaging:** distroless static image + a Helm chart whose RBAC is a
  **namespace-scoped Role** granting only `pods` get/list/watch + `pods/exec`
  create.

## Scope boundaries

- **In:** the gateway service (list + attach + auth-gate), frontend, Helm chart,
  CI, container image, tests.
- **Out (separate Frank batch):** the Frank ArgoCD app, Authentik forward-auth
  wiring, and the homepage tile. In this batch auth is just "trust a configurable
  header"; the chart exposes the header name + optional Ingress so the Frank
  wiring is pure configuration later.
- **Out (later C+ follow-ups):** the read-only kanban view and the broader
  dashboard/GUI.

## Verification (two-tier, per the spec)

- **Tier 1 — CI, no cluster:** unit tests cover config, the run lister (against
  `client-go/kubernetes/fake`), the auth middleware, the `terminalSession`
  Read/Write/Next + protocol demux, the exec-request builder, the ws handshake +
  auth gate, and static serving; plus `helm lint`/golden `helm template` and an
  image smoke (`/healthz`). The real `pods/exec` stream is **not** unit-testable
  (`envtest` has no kubelet).
- **Tier 2 — post-merge, manual (Phase 8):** deploy the chart against a real
  cluster with a labelled tmux probe pod and prove list + browser attach + resize
  + auth-reject end-to-end. This is the spec's real-path proof and is back-loaded
  so the PR ships with everything CI-verifiable green.

## Dependencies & ordering

Phases 2, 3, 4, 6 fan out from the Phase 1 bootstrap; 5 needs the APIs (2, 4);
7 needs the app + chart (5, 6); the manual verification (8) is last and depends
on the image (7). No agentic phase depends on the manual phase.

## Notes

- The dev devcontainer profile carries **no GitHub token** — pushes/PRs run on
  the authenticated host per the fr-isolation contract; the container only
  build/test/lints.
- Before this plan can be **dispatched** (`fr apply`), the cross-repo spec must
  be reachable on `derio-net/frank`'s default branch (the frank umbrella spec is
  currently uncommitted). Planning and local execution do not require that.
