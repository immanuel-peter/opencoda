#!/usr/bin/env bash
# UC1 bursty agent trace against gateway.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

: "${CODA_NAMESPACE:=opencoda-system}"
: "${CODA_GATEWAY_PORT:=18090}"
: "${CODA_TEST_MODEL:=hf://TinyLlama/TinyLlama-1.1B-Chat-v1.0}"

# shellcheck source=hack/lib/e2e-gateway.sh
source "$ROOT/hack/lib/e2e-gateway.sh"

token="$(e2e_create_token)"
e2e_start_gateway_port_forward
trap e2e_stop_gateway_port_forward EXIT

kubectl uncordon "$(kubectl get nodes -l opencoda.dev/gpu=true -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)" 2>/dev/null || true
e2e_patch_desired_replicas 1
for _ in $(seq 1 60); do
  if kubectl wait --for=condition=Ready pod -l "opencoda.dev/endpoint=${CODA_ENDPOINT_NAME}" -n "$CODA_ENDPOINT_NS" --timeout=10s 2>/dev/null; then
    break
  fi
  e2e_patch_desired_replicas 1
  sleep 2
done

: "${UC1_BURSTS:=5}"
: "${UC1_BURST_SIZE:=8}"
: "${UC1_IDLE_SEC:=30}"

go run ./test/e2e/loadgen \
  -gateway "http://127.0.0.1:${CODA_GATEWAY_PORT}" \
  -token "$token" \
  -model "$CODA_TEST_MODEL" \
  -bursts "${UC1_BURSTS:-5}" \
  -burst-size "${UC1_BURST_SIZE:-8}" \
  -idle-sec "${UC1_IDLE_SEC:-30}"
