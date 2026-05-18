#!/usr/bin/env bash
# Dump cluster diagnostics from the e2e environment. Designed to run on
# failure — every command is best-effort so a missing namespace or CRD
# doesn't mask the original error.

set -u

: "${E2E_NS:?}"

echo "==================== NAMESPACES ===================="
kubectl get ns || true

echo "==================== ALL IN ${E2E_NS} ===================="
kubectl -n "${E2E_NS}" get all || true

echo "==================== EVENTS (${E2E_NS}) ===================="
kubectl -n "${E2E_NS}" get events --sort-by=.lastTimestamp || true

echo "==================== POD DESCRIBE ===================="
for pod in $(kubectl -n "${E2E_NS}" get pods -o name 2>/dev/null); do
  echo "------ describe ${pod} ------"
  kubectl -n "${E2E_NS}" describe "${pod}" || true
done

echo "==================== POD LOGS ===================="
for pod in $(kubectl -n "${E2E_NS}" get pods -o name 2>/dev/null); do
  echo "------ logs ${pod} ------"
  kubectl -n "${E2E_NS}" logs "${pod}" --tail=200 --all-containers || true
done

echo "==================== CRDS ===================="
kubectl get crd | grep -E '(stuttgart-things|machinery)' || true

echo "==================== TEST FIXTURES ===================="
for kind in harvestervm storageplatform networkintegration; do
  echo "------ ${kind} ------"
  kubectl get "${kind}.resources.stuttgart-things.com" -A -o yaml || true
done

echo "==================== RBAC ===================="
kubectl get clusterrole machinery-e2e-reader -o yaml || true
kubectl get clusterrolebinding machinery-e2e-reader -o yaml || true

echo "==================== KIND NODES ===================="
kind get clusters || true
docker ps --filter "label=io.x-k8s.kind.cluster" || true
