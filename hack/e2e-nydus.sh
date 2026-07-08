#!/usr/bin/env bash
# Nydus + cachefill validation on EKS GPU nodes.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

: "${EKS_CLUSTER_NAME:?set EKS_CLUSTER_NAME}"
: "${AWS_REGION:=us-east-1}"
: "${FAKEVLLM_IMAGE:=ghcr.io/immanuel-peter/opencoda/coda-fakevllm:latest}"
: "${NODE_AGENT_IMAGE:=ghcr.io/immanuel-peter/opencoda/coda-node-agent:latest}"
: "${CONTROLLER_IMAGE:?set CONTROLLER_IMAGE}"
: "${GATEWAY_IMAGE:?set GATEWAY_IMAGE}"
: "${NYDUS_IMAGE:=${FAKEVLLM_IMAGE}-nydus}"
: "${GATEWAY_IMAGE:?set GATEWAY_IMAGE}"

echo "==> converting fakevllm image to Nydus (requires nydusify in PATH)"
if command -v nydusify >/dev/null 2>&1; then
  go run ./cmd/coda image convert \
    --source "$FAKEVLLM_IMAGE" \
    --target "$NYDUS_IMAGE"
else
  echo "nydusify not installed; skipping convert (use pre-built ${NYDUS_IMAGE})"
fi

export CODA_ENABLE_NODEAGENT=1
export CODA_ENGINE_IMAGE="$NYDUS_IMAGE"
export CODA_E2E_FIXTURE=gpu-smoke.yaml
export CODA_GATEWAY_TEST=0

chmod +x hack/e2e-eks-gpu.sh hack/e2e-eks.sh tmp/eks/add-gpu-nodegroup.sh
./tmp/eks/add-gpu-nodegroup.sh

helm upgrade opencoda "$ROOT/charts/opencoda" -n opencoda-system \
  --reuse-values \
  --set controllerManager.image="$CONTROLLER_IMAGE" \
  --set gateway.image="$GATEWAY_IMAGE" \
  --set controllerManager.engineImage="$NYDUS_IMAGE" \
  --set nodeAgent.enabled=true \
  --set nodeAgent.image="$NODE_AGENT_IMAGE" \
  --set nodeAgent.images="$NYDUS_IMAGE" \
  --set garage.enabled=false \
  --set dcgmExporter.enabled=false \
  --wait --timeout 10m

kubectl apply -f "$ROOT/test/e2e/fixtures/gpu-smoke.yaml"

kubectl -n opencoda-system rollout status daemonset/coda-node-agent --timeout=300s
kubectl -n opencoda-system logs daemonset/coda-node-agent --tail=100

echo "Nydus cachefill validation launched (check node-agent logs for pull/prefetch)"
