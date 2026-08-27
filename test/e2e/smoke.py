#!/usr/bin/env python3
"""In-cluster e2e smoke test for the runs-fr gateway — the staging-gate's Tier-2 check.

Runs as a Job INSIDE the staging vCluster (so it can reach the gateway's ClusterIP). It
self-provisions a fixture "run pod" (alpine + tmux + fr.run/* labels) — until Component A
produces real run pods — then asserts the gateway's real behaviour:
  1. GET /api/runs with NO auth header        -> 403
  2. GET /api/runs with X-Forwarded-User       -> 200 and lists the probe
  3. WS /api/runs/<probe>/attach               -> binary stdin + text resize -> tmux echoes back
Exit 0 = pass (gate promotes), non-zero = fail (gate blocks promotion).

Env: GATEWAY_URL (default http://runs-fr.runs-fr.svc), SMOKE_NAMESPACE (default runs-fr).
Requires a ServiceAccount with pods get/list/create/delete in SMOKE_NAMESPACE (see rbac.yaml).
"""
from __future__ import annotations

import asyncio
import json
import os
import sys
import time
import urllib.error
import urllib.request

import websockets
from kubernetes import client, config

NS = os.environ.get("SMOKE_NAMESPACE", "runs-fr")
GW = os.environ.get("GATEWAY_URL", "http://runs-fr.runs-fr.svc").rstrip("/")
WS = GW.replace("https://", "wss://").replace("http://", "ws://")
PROBE = "smoke-probe"
HDR = {"X-Forwarded-User": "smoke"}


def _http(path: str, headers: dict | None = None):
    return urllib.request.urlopen(urllib.request.Request(GW + path, headers=headers or {}), timeout=10)


def ensure_probe() -> client.CoreV1Api:
    config.load_incluster_config()
    v1 = client.CoreV1Api()
    pod = client.V1Pod(
        metadata=client.V1ObjectMeta(
            name=PROBE,
            labels={
                "fr.run/id": PROBE, "fr.run/plan": "smoke", "fr.run/phase": "1",
                "fr.run/branch": "smoke", "fr.run/harness": "claude",
            },
        ),
        spec=client.V1PodSpec(
            restart_policy="Never",
            containers=[client.V1Container(
                name="shell", image="alpine:3.20",
                command=["sh", "-c", f"apk add --no-cache tmux >/dev/null 2>&1 && tmux new-session -d -s {PROBE} && sleep infinity"],
            )],
        ),
    )
    try:
        v1.create_namespaced_pod(NS, pod)
    except client.ApiException as e:
        if e.status != 409:  # already exists is fine
            raise
    for _ in range(60):
        if v1.read_namespaced_pod(PROBE, NS).status.phase == "Running":
            time.sleep(4)  # let tmux come up
            return v1
        time.sleep(2)
    raise SystemExit("probe pod did not reach Running")


def cleanup(v1: client.CoreV1Api) -> None:
    try:
        v1.delete_namespaced_pod(PROBE, NS)
    except Exception:  # noqa: BLE001 — best-effort; reset task drops the ns anyway
        pass


async def _ws_attach() -> bool:
    async with websockets.connect(f"{WS}/api/runs/{PROBE}/attach", extra_headers=HDR, open_timeout=15) as ws:
        await ws.send(json.dumps({"resize": {"cols": 80, "rows": 24}}))  # TEXT frame
        await asyncio.sleep(0.5)
        await ws.send(b"echo SMOKE_OK_$((3+4))\n")                        # BINARY stdin
        out = b""
        try:
            for _ in range(40):
                m = await asyncio.wait_for(ws.recv(), timeout=4)
                out += m if isinstance(m, (bytes, bytearray)) else m.encode()
                if b"SMOKE_OK_7" in out:
                    break
        except asyncio.TimeoutError:
            pass
        ok = b"SMOKE_OK_7" in out
        print(f"  ws-attach: {'PASS' if ok else 'FAIL'}")
        return ok


def checks() -> bool:
    try:
        _http("/api/runs")
        print("  noauth: FAIL (expected 403, got 200)")
        ok1 = False
    except urllib.error.HTTPError as e:
        ok1 = e.code == 403
        print(f"  noauth: {'PASS' if ok1 else 'FAIL'} (status {e.code})")
    body = _http("/api/runs", HDR).read().decode()
    ok2 = PROBE in body
    print(f"  list:   {'PASS' if ok2 else 'FAIL'} ({body[:160]})")
    ok3 = asyncio.run(_ws_attach())
    return ok1 and ok2 and ok3


def main() -> int:
    print(f"smoke: gateway={GW} ns={NS}")
    v1 = ensure_probe()
    try:
        ok = checks()
    finally:
        cleanup(v1)
    print("smoke: PASS" if ok else "smoke: FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
