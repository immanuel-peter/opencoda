#!/usr/bin/env bash
# Real vLLM + LMCache wedge validation on EKS g5.
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
: "${CODA_FAKE_HEALTH:=1}"
: "${VLLM_IMAGE:=vllm/vllm-openai:latest}"

chmod +x tmp/eks/add-gpu-nodegroup.sh hack/e2e-eks.sh hack/lib/garage-bootstrap.sh
./tmp/eks/add-gpu-nodegroup.sh

export CODA_ENGINE_IMAGE="$VLLM_IMAGE"
export CODA_E2E_FIXTURE=gpu-vllm.yaml
export CODA_ENABLE_GARAGE=1
export CODA_ENABLE_DCGM=1
export CODA_GATEWAY_TEST=1
export CODA_TEST_MODEL="hf://Qwen/Qwen2.5-0.5B-Instruct"
./hack/e2e-eks.sh

echo "==> waiting for real vLLM endpoint"
kubectl wait --for=condition=Ready pod -l opencoda.dev/endpoint=demo-vllm -n default --timeout=900s
kubectl get codaendpoint demo-vllm -n default -o yaml | grep -A6 'coldStart:' || true
kubectl get codaendpoint demo-vllm -n default -o yaml | grep 'kvHitRate:' || true
echo "real vLLM wedge validation complete"
