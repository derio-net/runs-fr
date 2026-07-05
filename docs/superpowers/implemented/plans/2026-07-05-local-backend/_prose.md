# runs-fr local backend (spec: agentic-stoa/cnc-fr 2026-07-05-runs-fr-live-attach)

runs-fr gains a `local` backend: lister/resolver over `tmux ls`/`has-session`,
attacher = `tmux attach -t =<id>` on a creack/pty PTY bridged to the existing
ws protocol (binary=stdio, text=resize envelope). BACKEND=kube stays the
default — existing cluster deploys see zero change. RUNNER_NAME surfaces as an
optional `runner` field on Run so aggregated multi-runner lists are
distinguishable. Scope discipline: no CNC knowledge, no wrapper semantics —
list + attach only. v0.1.0 → v0.2.0.
