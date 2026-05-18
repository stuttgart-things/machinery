#!/usr/bin/env bash
# Port-forward machinery's gRPC port out of the kind cluster and run the
# Go e2e suite against it. Lives in a script (not inline in Taskfile.yaml)
# because mvdan/sh — Task's embedded shell — doesn't expose `$!` under
# `set -u`, so the inline form fails on CI with `!: unbound variable`.
# Real bash handles it correctly.

set -euo pipefail

: "${E2E_NS:?}"

kubectl -n "${E2E_NS}" port-forward svc/machinery 50051:50051 \
  >/tmp/machinery-pf.log 2>&1 &
PF_PID=$!
trap 'kill ${PF_PID} 2>/dev/null || true' EXIT

# `kubectl port-forward` returns immediately but the listener takes a beat
# to bind. Poll the socket so we don't race the first dial.
for _ in $(seq 1 30); do
  if (echo > /dev/tcp/127.0.0.1/50051) 2>/dev/null; then break; fi
  sleep 1
done

E2E_GRPC_ADDR=localhost:50051 go test -tags=e2e -v -count=1 ./tests/e2e/...
