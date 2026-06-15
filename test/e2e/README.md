# runs-fr e2e smoke test

The in-cluster Tier-2 check for the **staging-gate** (frank
`docs/superpowers/specs/2026-06-15--cicd--staging-vcluster-gate-design.md`). Built per-commit as
`ghcr.io/derio-net/runs-fr-smoke:sha-<commit>` by `.github/workflows/build.yml`, and run as a Job
inside the staging vCluster by Frank's Tekton `staging-gate` Pipeline.

`smoke.py` self-provisions a fixture run-pod (`alpine` + `tmux`, `fr.run/*` labels — stand-in
until Component A makes real run pods), then asserts the gateway's real behaviour:

1. `GET /api/runs` with **no** auth header → **403**
2. `GET /api/runs` with `X-Forwarded-User` → **200**, lists the probe
3. `WS /api/runs/<probe>/attach` → binary stdin + text resize → tmux echoes the command back

Exit 0 = pass (gate promotes); non-zero = blocks promotion.

**Env:** `GATEWAY_URL` (default `http://runs-fr.runs-fr.svc`), `SMOKE_NAMESPACE` (default `runs-fr`).

**Gate wiring (frank side / P7):** the gate's `run-smoke` task applies `rbac.yaml` into the
vCluster smoke namespace and launches the smoke Job with `serviceAccountName: runs-fr-smoke`
(the SA grants the pods CRUD the smoke needs to create its fixture). The repository_dispatch the
build workflow fires carries `client_payload.sha` — the gate's CEL/binding should validate it
matches `^[a-z0-9]+$` before it flows into the task shells.
