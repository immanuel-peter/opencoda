#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export CODA_E2E=1
export CODA_FAKE_HEALTH=1

kind delete cluster --name opencoda-e2e 2>/dev/null || true
kind create cluster --name opencoda-e2e

if [[ "${CODA_SKIP_DOCKER_BUILD:-}" != "1" ]]; then
  docker build -t opencoda-controller:latest -f "$ROOT/hack/Dockerfile.controller" "$ROOT" &
  ctrl_pid=$!
  docker build -t opencoda-fakevllm:latest -f "$ROOT/hack/Dockerfile.fakevllm" "$ROOT" &
  fake_pid=$!
  wait "$ctrl_pid" "$fake_pid"
fi

kind load docker-image opencoda-controller:latest --name opencoda-e2e
kind load docker-image opencoda-fakevllm:latest --name opencoda-e2e

kubectl label node --all opencoda.dev/control-plane=true --overwrite

# Chart templates include a Namespace; do not combine with --create-namespace.
helm upgrade --install opencoda "$ROOT/charts/opencoda" \
  --namespace opencoda-system \
  --set controllerManager.image=opencoda-controller:latest \
  --set gateway.image=opencoda-controller:latest \
  --set controllerManager.fakeHealth=true \
  --set garage.enabled=false \
  --set spegel.enabled=false \
  --set providers.static.enabled=true

echo "==> installing CRDs"
kubectl apply -f "$ROOT/config/crd/bases/"

kubectl apply -f "$ROOT/test/e2e/fixtures/minimal.yaml"
kubectl wait --for=condition=established crd/gpupools.opencoda.dev --timeout=120s || true

go test ./test/e2e -count=1
