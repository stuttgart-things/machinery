#!/usr/bin/env bash
# Bring up the e2e environment: kind cluster, machinery image built with ko
# and loaded directly into kind (no registry needed — the container is the
# only OCI artifact we ship, unlike Crossplane functions). Then install the
# minimal test CRDs, deploy machinery via its KCL manifests, bind RBAC, and
# apply the test fixtures.
#
# Idempotent: any leftover cluster is scrubbed first.

set -euo pipefail

: "${KIND_CLUSTER:?}"
: "${E2E_IMAGE:?}"
: "${E2E_NS:?}"

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
MANIFESTS="${REPO_ROOT}/tests/e2e/manifests"
KCL_PROFILE="${REPO_ROOT}/tests/e2e/kcl-profile.yaml"

dump_on_failure() {
  local code=$?
  if [ "${code}" -ne 0 ]; then
    echo "==> up.sh failed (exit ${code})" >&2
    bash "$(dirname "$0")/dump.sh" || true
  fi
}
trap dump_on_failure EXIT

echo "==> scrub leftover cluster"
kind delete cluster --name "${KIND_CLUSTER}" >/dev/null 2>&1 || true

echo "==> create kind cluster"
kind create cluster --name "${KIND_CLUSTER}"

echo "==> build machinery image with ko (local daemon)"
# KO_DOCKER_REPO=ko.local makes ko build into the local Docker daemon and
# emit the resulting image ref on stdout. We retag to a stable name so the
# KCL profile can reference it without knowing the sha digest.
KO_IMAGE=$(cd "${REPO_ROOT}" && KO_DOCKER_REPO=ko.local ko build --local --bare --tags e2e .)
echo "==> ko produced ${KO_IMAGE}"
docker tag "${KO_IMAGE}" "${E2E_IMAGE}"

echo "==> load ${E2E_IMAGE} into kind"
kind load docker-image "${E2E_IMAGE}" --name "${KIND_CLUSTER}"

echo "==> install test CRDs"
kubectl apply -f "${MANIFESTS}/crds.yaml"
# Give the API server a beat to register the new types before we create CRs.
kubectl wait --for=condition=Established \
  crd/harvestervms.resources.stuttgart-things.com \
  crd/storageplatforms.resources.stuttgart-things.com \
  crd/networkintegrations.resources.stuttgart-things.com \
  --timeout=60s

echo "==> render + apply KCL manifests"
# `kcl run` emits a single doc with `manifests: [...]`; split it and feed
# each element to kubectl. Mirrors the snippet in kcl/README.md.
kcl run "${REPO_ROOT}/kcl/main.k" -Y "${KCL_PROFILE}" \
  | yq eval '.manifests[] | splitDoc' - \
  | kubectl apply -f -

echo "==> bind RBAC so machinery can list the test CRs"
kubectl apply -f "${MANIFESTS}/rbac.yaml"

echo "==> apply fixtures"
kubectl apply -f "${MANIFESTS}/fixtures.yaml"
# `kubectl apply` ignores the `status` stanza when a CRD declares the status
# subresource. Replay each fixture's status via `kubectl patch --subresource`
# so the values machinery extracts via statusFields are actually present.
# `status.conditions[type=Ready]` is what machinery's getResourceStatus()
# checks to set the Ready boolean — mirrors how real Crossplane CRs signal it.
kubectl patch harvestervm.resources.stuttgart-things.com e2e-vm-ready -n default \
  --subresource=status --type=merge \
  -p '{"status":{"vm":{"name":"e2e-vm-ready","ready":true},"volume":{"ready":true},"cloudInit":{"ready":true},"conditions":[{"type":"Ready","status":"True","lastTransitionTime":"2026-01-01T00:00:00Z","reason":"E2E","message":"ready"}]}}'
kubectl patch harvestervm.resources.stuttgart-things.com e2e-vm-pending -n default \
  --subresource=status --type=merge \
  -p '{"status":{"vm":{"name":"e2e-vm-pending","ready":false},"volume":{"ready":true},"cloudInit":{"ready":false},"conditions":[{"type":"Ready","status":"False","lastTransitionTime":"2026-01-01T00:00:00Z","reason":"E2E","message":"not ready"}]}}'
kubectl patch storageplatform.resources.stuttgart-things.com e2e-storage -n default \
  --subresource=status --type=merge \
  -p '{"status":{"installed":true,"observedVersion":"1.2.3","conditions":[{"type":"Ready","status":"True","lastTransitionTime":"2026-01-01T00:00:00Z","reason":"E2E","message":"ready"}]}}'
kubectl patch networkintegration.resources.stuttgart-things.com e2e-network -n default \
  --subresource=status --type=merge \
  -p '{"status":{"installed":true,"observedVersion":"4.5.6","conditions":[{"type":"Ready","status":"True","lastTransitionTime":"2026-01-01T00:00:00Z","reason":"E2E","message":"ready"}]}}'

echo "==> wait for machinery rollout"
kubectl -n "${E2E_NS}" rollout status deploy/machinery --timeout=180s

echo "==> e2e:up done"
