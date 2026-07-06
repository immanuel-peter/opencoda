#!/usr/bin/env bash
# GPU validation on EKS: static pool + fakevllm engine on a g5 node.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

: "${AWS_REGION:=us-east-1}"
: "${EKS_CLUSTER_NAME:?set EKS_CLUSTER_NAME}"
: "${CODA_FAKE_HEALTH:=1}"
: "${FAKEVLLM_IMAGE:=ghcr.io/immanuel-peter/opencoda/coda-fakevllm:latest}"

chmod +x tmp/eks/add-gpu-nodegroup.sh hack/e2e-eks.sh
./tmp/eks/add-gpu-nodegroup.sh

export CODA_ENGINE_IMAGE="$FAKEVLLM_IMAGE"
export CODA_E2E_FIXTURE=gpu-smoke.yaml
./hack/e2e-eks.sh

echo "==> waiting for endpoint pod on GPU node"
kubectl wait --for=condition=Ready pod -l opencoda.dev/endpoint=demo-vllm -n default --timeout=300s
kubectl get pod -l opencoda.dev/endpoint=demo-vllm -n default -o wide

echo "==> GPUPool status"
kubectl get gpupool onprem-static -o yaml | grep -A20 '^status:'

POD=$(kubectl get pod -l opencoda.dev/endpoint=demo-vllm -n default -o jsonpath='{.items[0].metadata.name}')
NODE=$(kubectl get pod "$POD" -n default -o jsonpath='{.spec.nodeName}')
echo "==> endpoint pod ${POD} scheduled on ${NODE}"
kubectl get node "$NODE" -L opencoda.dev/gpu,opencoda.dev/pool

echo ""
echo "GPU smoke test passed: fakevllm pod is Ready on a labeled GPU node."
