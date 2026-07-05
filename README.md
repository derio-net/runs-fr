# runs-fr

Kubernetes-native, **portable** web shell gateway for agentic runs. It lists
live run pods (selected by an `fr.run/*` label) and lets you attach to a pod's
named session in the browser, bridging the Kubernetes `pods/exec` subresource to
an [xterm.js](https://xtermjs.org/) terminal.

`runs-fr` is the **Component C** slice of the k8s-native agentic runs design.
It is deliberately cluster-agnostic: the namespace it watches, the label
selector, and the trusted auth header are all configuration, so the same
image/chart installs into any cluster.

- **Server (Go):** a ~300-line exec↔websocket bridge on `k8s.io/client-go`
  (`remotecommand` + `kubernetes`) and `github.com/coder/websocket`. Exec uses
  `remotecommand.NewFallbackExecutor(WebSocket, SPDY)` for cluster-version
  robustness.
- **Frontend:** `@xterm/xterm` + `@xterm/addon-fit`, bundled with esbuild and
  embedded into the binary via `embed.FS` (no CDN, no separate asset server).
- **Packaging:** a distroless static image and a Helm chart whose RBAC is a
  namespace-scoped Role granting only `pods` get/list/watch + `pods/exec`
  create.

## ⚠️ Security contract — read this first

**runs-fr performs NO authentication of its own.** It trusts a single request
header (`AUTH_HEADER`, default `X-Forwarded-User`) to identify the
authenticated user. The liveness/readiness probes (`/healthz`, `/readyz`) and
the static frontend assets (`/`, `/bundle.js`, `/xterm.css`) are served without
that header — they carry no run data. Every `/api/*` route — including
`GET /api/runs` and the `pods/exec` attach websocket — is gated behind it.

Because the gateway trusts that header, it **MUST sit behind a forward-auth
proxy** (Authentik / oauth2-proxy / Traefik `forwardAuth`, etc.) that **SETS
AND OVERWRITES the header on every request**, including stripping any
client-supplied copy.

- A proxy that merely **adds** the header (without overwriting) lets a client
  send its own `X-Forwarded-User: someone-else` and have it survive — full
  spoof, reaching `pods/exec`.
- Exposing the Service or Ingress **directly** (no proxy) lets any client set
  the header and attach to run pods.

Treat reaching this Service as equivalent to a shell in every run pod. Lock the
network path down accordingly (see [Network posture](#network-posture)).

## Configuration

All configuration is via environment variables (read at startup by
`internal/config`):

| Env var          | Default            | Required | Description                                                                 |
| ---------------- | ------------------ | -------- | --------------------------------------------------------------------------- |
| `BACKEND`        | `kube`             | no       | Attach backend: `kube` (pods/exec) or `local` (tmux on the gateway's host). |
| `NAMESPACE`      | —                  | kube only | Kubernetes namespace to watch for run pods (the `pods/exec` scope).        |
| `ADDR`           | `:8080`            | no       | Listen address.                                                             |
| `LABEL_SELECTOR` | `fr.run/id`        | no       | Label selector identifying run pods (kube backend).                         |
| `AUTH_HEADER`    | `X-Forwarded-User` | no       | Trusted header carrying the authenticated user (set by the proxy).          |
| `KUBECONFIG`     | _(in-cluster)_     | no       | Path to a kubeconfig file; empty uses the in-cluster ServiceAccount config. |
| `TMUX_SOCKET`    | _(tmux default)_   | no       | Explicit tmux socket path (`tmux -S`) — local backend.                      |
| `RUNNER_NAME`    | _(empty)_          | no       | Tags served runs (`"runner"` field) — local backend, for aggregated multi-runner lists. |

### Local backend (tmux)

`BACKEND=local` serves the tmux sessions of the gateway's **own host** as
runs: `GET /api/runs` lists `tmux ls` (one Run per session, `id` = session
name, sparse metadata), and the attach route bridges `tmux attach -t =<id>`
over a local PTY to the same binary/`{"resize":…}` websocket protocol. No
cluster, no Docker socket — the intended shape is a runner container that
bundles this gateway, tmux, and whatever creates the sessions (e.g. CNC's
`cnc-runner` wake receiver, which names each session after its run id).

The **security contract is unchanged**: every `/api/*` route still trusts
`AUTH_HEADER` alone, so a forward-auth proxy that sets-and-overwrites the
header MUST front the gateway — a reachable local-backend gateway is a
read-write shell on every session it hosts.

## Install (Helm)

The chart lives in [`charts/runs-fr`](charts/runs-fr). It defaults to a
`ClusterIP` Service with Ingress disabled — safe-by-default given the security
contract above.

```sh
helm install runs-fr ./charts/runs-fr \
  --namespace fr-runs --create-namespace \
  --set namespace=fr-runs            # the namespace to WATCH for run pods
```

The chart creates a ServiceAccount bound to a namespace-scoped Role granting
only `pods` (get/list/watch) and `pods/exec` (create). Key values (see
[`values.yaml`](charts/runs-fr/values.yaml) for the full set and inline
security notes):

| Value                   | Default                     | Description                                                            |
| ----------------------- | --------------------------- | ---------------------------------------------------------------------- |
| `image.repository`      | `ghcr.io/derio-net/runs-fr` | Image; `image.tag` defaults to the chart `appVersion`.                 |
| `namespace`             | _(release ns)_              | Namespace to watch / scope the `pods/exec` Role to.                    |
| `labelSelector`         | `fr.run/id`                 | `LABEL_SELECTOR`.                                                      |
| `authHeader`            | `X-Forwarded-User`          | `AUTH_HEADER`.                                                         |
| `service.type`          | `ClusterIP`                 | Keep ClusterIP unless a forward-auth proxy fronts it.                  |
| `ingress.enabled`       | `false`                     | Enable **only** behind a forward-auth proxy.                           |
| `networkPolicy.enabled` | `false`                     | Opt-in, default-deny; restricts ingress to `networkPolicy.from` peers. |

### Network posture

The Service is `ClusterIP` by default — no direct external exposure. The
opt-in `NetworkPolicy` is **default-deny**: when `networkPolicy.enabled=true`,
ingress to the gateway is restricted to the peers listed in
`networkPolicy.from` (your forward-auth proxy) on the container port; an empty
`from` denies all ingress. Set `from` to your proxy's selector so the gateway
is reachable only via the proxy that sets the trusted header:

```yaml
networkPolicy:
  enabled: true
  from:
    - podSelector:
        matchLabels:
          app.kubernetes.io/name: forward-auth-proxy
```

> **Frank deploy wiring is a separate batch.** The Authentik forward-auth
> wiring, the ArgoCD app, the public Ingress, and the homepage tile are
> configured in the `frank` umbrella repo, not here. This repo ships the image,
> the chart, and the configuration surface those will set.

## Development

This repo uses the `fr` isolation contract: feature work runs in an isolated
git worktree + devcontainer, never the base checkout. The `dev` profile carries
no GitHub token — builds/tests/lints run in the container; pushes and PRs happen
on the authenticated host.

```sh
fr isolation exec -- make test        # go test ./... -race
fr isolation exec -- make lint        # go vet + golangci-lint
fr isolation exec -- make frontend    # esbuild bundle into cmd/runs-fr/static/
fr isolation exec -- make build       # frontend + static Go binary into bin/
```

The frontend bundle is embedded via `//go:embed static/*` into the
`cmd/runs-fr` package, so **`make frontend` must run before any `go build`/`go
test`** on a fresh checkout (`bundle.js` is generated and git-ignored). CI runs
the frontend step first for exactly this reason.

## Container image & release

The [`Dockerfile`](Dockerfile) is a three-stage build: a Node stage bundles the
frontend, a Go stage compiles a static (`CGO_ENABLED=0`) binary with the bundle
embedded, and the final image is `gcr.io/distroless/static:nonroot` running as
`nonroot` on port 8080.

```sh
docker build -t runs-fr:dev .
docker run -p 8080:8080 -e NAMESPACE=x runs-fr:dev
curl localhost:8080/healthz        # -> ok
```

Pushing a `v*` tag triggers [`.github/workflows/release.yml`](.github/workflows/release.yml),
which builds and pushes the image to `ghcr.io/derio-net/runs-fr` with three
tags: the pushed git tag (e.g. `v0.1.0`), the bare semver version (`0.1.0`, so
the chart's `appVersion`-derived default resolves), and the commit SHA
(`sha-<short>`).
