#!/usr/bin/env bash
# Phase 1 optional follow-ups on existing EKS: vLLM+Garage, DCGM, UC1, Nydus.
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
: "${CONTROLLER_IMAGE:?set CONTROLLER_IMAGE}"
: "${GATEWAY_IMAGE:?set GATEWAY_IMAGE}"
: "${FAKEVLLM_IMAGE:?set FAKEVLLM_IMAGE}"
: "${NODE_AGENT_IMAGE:=352899296530.dkr.ecr.us-east-1.amazonaws.com/opencoda/coda-node-agent:latest}"

GPU_NODE="${GPU_NODE:-$(kubectl get nodes -l opencoda.dev/gpu=true -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)}"

keep_gpu_warm() {
  [[ -n "$GPU_NODE" ]] || return 0
  kubectl uncordon "$GPU_NODE" 2>/dev/null || true
  kubectl -n default annotate codaendpoint demo-vllm opencoda.dev/desired-replicas=1 --overwrite 2>/dev/null || true
}

echo "==> Leg 1/4: real vLLM + Garage + DCGM"
chmod +x tmp/eks/add-gpu-nodegroup.sh hack/e2e-eks.sh hack/lib/garage-bootstrap.sh
./tmp/eks/add-gpu-nodegroup.sh

helm upgrade --install opencoda "$ROOT/charts/opencoda" -n opencoda-system \
  --reuse-values \
  --set controllerManager.image="$CONTROLLER_IMAGE" \
  --set gateway.image="$GATEWAY_IMAGE" \
  --set controllerManager.engineImage=vllm/vllm-openai:latest \
  --set garage.enabled=true \
  --set dcgmExporter.enabled=true \
  --set nodeAgent.enabled=false \
  --wait --timeout 10m

kubectl apply -f "$ROOT/test/e2e/fixtures/gpu-vllm.yaml"
kubectl patch codaendpoint demo-vllm -n default --type=merge \
  -p '{"spec":{"scaling":{"minReplicas":1,"scaleToZeroAfter":"30m"}}}' 2>/dev/null || true
kubectl patch bufferpolicy default --type=merge \
  -p '{"spec":{"target":{"minWarmGPUs":1,"maxWarmGPUs":1},"scaleDown":{"stabilizationWindow":"30m"}}}' 2>/dev/null || true
keep_gpu_warm
./hack/lib/garage-bootstrap.sh

echo "==> waiting for real vLLM pod (up to 20m; image pulls + model load)"
for _ in $(seq 1 40); do
  keep_gpu_warm
  if kubectl wait --for=condition=Ready pod -l opencoda.dev/endpoint=demo-vllm -n default --timeout=15s 2>/dev/null; then
    break
  fi
  sleep 30
done
kubectl get pod -n default -l opencoda.dev/endpoint=demo-vllm -o wide

echo "==> DCGM metrics smoke"
DCGM_POD="$(kubectl -n opencoda-system get pod -l app=dcgm-exporter -o jsonpath='{.items[0].metadata.name}')"
kubectl -n opencoda-system port-forward "$DCGM_POD" 19400:9400 >/dev/null 2>&1 &
DCGM_PF=$!
trap 'kill "$DCGM_PF" 2>/dev/null || true' EXIT
sleep 2
curl -sf http://127.0.0.1:19400/metrics | grep -E 'DCGM_FI_DEV_GPU_UTIL|DCGM_FI_DEV_XID' | head -5 || echo "WARN: no DCGM sample lines yet"

echo "==> Leg 2/4: LMCache tier spill"
chmod +x hack/lib/lmcache-tier-spill.sh
# Warm KV via gateway before restart.
# shellcheck source=hack/lib/e2e-gateway.sh
source "$ROOT/hack/lib/e2e-gateway.sh"
token="$(e2e_create_token)"
e2e_start_gateway_port_forward
CODA_TEST_MODEL="hf://Qwen/Qwen2.5-0.5B-Instruct"
e2e_wait_gateway_success "$token" "$CODA_TEST_MODEL" 300 || true
./hack/lib/lmcache-tier-spill.sh
e2e_stop_gateway_port_forward

echo "==> Leg 3/4: UC1 utilization soak"
CODA_TEST_MODEL="hf://Qwen/Qwen2.5-0.5B-Instruct" UC1_IDLE_SEC=10 UC1_BURSTS=4 UC1_BURST_SIZE=6 ./hack/e2e-uc1.sh

echo "==> Leg 4/4: Nydus cachefill"
chmod +x hack/e2e-nydus.sh
export NODE_AGENT_IMAGE FAKEVLLM_IMAGE CONTROLLER_IMAGE GATEWAY_IMAGE
./hack/e2e-nydus.sh

echo "Phase 1 follow-ups complete"
