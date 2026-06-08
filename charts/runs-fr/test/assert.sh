#!/usr/bin/env bash
#
# Golden assertions for the runs-fr Helm chart.
#
# Runs `helm lint` + `helm template` (default and ingress-enabled renders) and
# greps the output for the security-critical invariants from the plan:
#   * Role grants pods/exec create
#   * NO ClusterRole / ClusterRoleBinding (namespace-scoped RBAC only)
#   * Service type is ClusterIP by default
#   * Deployment is hardened (readOnlyRootFilesystem + capabilities drop ALL)
#   * Ingress renders ONLY when ingress.enabled=true
#
# helm is not available in the runs-fr exec pod, so this script runs in CI (and
# in the dev devcontainer). Exits non-zero on the first failed assertion.
set -euo pipefail

CHART_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RELEASE="runs-fr-test"

pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; exit 1; }

# assert_has <description> <haystack> <needle>
assert_has() {
  if printf '%s' "$2" | grep -qF -- "$3"; then pass "$1"; else fail "$1 (expected to find: $3)"; fi
}

# assert_lacks <description> <haystack> <needle>
assert_lacks() {
  if printf '%s' "$2" | grep -qF -- "$3"; then fail "$1 (unexpected: $3)"; else pass "$1"; fi
}

echo "==> helm lint"
helm lint "$CHART_DIR"

echo "==> helm template (default)"
DEFAULT="$(helm template "$RELEASE" "$CHART_DIR")"

echo "==> assertions (default render)"
assert_has   "Role grants pods/exec"                  "$DEFAULT" "pods/exec"
assert_has   "Role kind present"                      "$DEFAULT" "kind: Role"
assert_lacks "no ClusterRole"                         "$DEFAULT" "kind: ClusterRole"
assert_lacks "no ClusterRoleBinding"                  "$DEFAULT" "kind: ClusterRoleBinding"
assert_has   "Service type ClusterIP by default"      "$DEFAULT" "type: ClusterIP"
assert_has   "Deployment readOnlyRootFilesystem"      "$DEFAULT" "readOnlyRootFilesystem: true"
assert_has   "Deployment allowPrivilegeEscalation"    "$DEFAULT" "allowPrivilegeEscalation: false"
assert_has   "Deployment runAsNonRoot"                "$DEFAULT" "runAsNonRoot: true"
assert_has   "Deployment drops ALL capabilities"      "$DEFAULT" "- ALL"
assert_has   "liveness probe /healthz"                "$DEFAULT" "/healthz"
assert_has   "readiness probe /readyz"                "$DEFAULT" "/readyz"
assert_lacks "ingress not rendered by default"        "$DEFAULT" "kind: Ingress"

echo "==> helm template (ingress.enabled=true)"
WITH_INGRESS="$(helm template "$RELEASE" "$CHART_DIR" --set ingress.enabled=true --set ingress.host=runs.example.com)"

echo "==> assertions (ingress render)"
assert_has   "ingress rendered when enabled"          "$WITH_INGRESS" "kind: Ingress"
assert_has   "ingress host set"                       "$WITH_INGRESS" "runs.example.com"
assert_lacks "still no ClusterRole with ingress on"   "$WITH_INGRESS" "kind: ClusterRole"

echo "==> all chart assertions passed"
