#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export CODA_E2E=1
export CODA_FAKE_HEALTH=1

kind create cluster --name opencoda-e2e || true
docker build -t opencoda-controller:latest -f "$ROOT/hack/Dockerfile.controller" "$ROOT"
docker build -t opencoda-fakevllm:latest -f "$ROOT/hack/Dockerfile.fakevllm" "$ROOT"
kind load docker-image opencoda-controller:latest --name opencoda-e2e
kind load docker-image opencoda-fakevllm:latest --name opencoda-e2e

helm upgrade --install opencoda "$ROOT/charts/opencoda" \
  --namespace opencoda-system --create-namespace \
  --set controllerManager.image=opencoda-controller:latest \
  --set gateway.image=opencoda-controller:latest \
  --set providers.static.enabled=true

kubectl apply -f "$ROOT/test/e2e/fixtures/minimal.yaml"
kubectl wait --for=condition=established crd/gpupools.opencoda.dev --timeout=120s || true

go test ./test/e2e -count=1
