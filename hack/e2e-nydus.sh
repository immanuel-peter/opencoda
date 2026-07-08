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

echo "==> converting fakevllm image to Nydus (requires nydusify in PATH)"
if command -v nydusify >/dev/null 2>&1; then
  go run ./cmd/coda image convert \
    --source "$FAKEVLLM_IMAGE" \
    --target "$NYDUS_IMAGE"
else
  echo "nydusify not installed; skipping convert (use pre-built ${NYDUS_IMAGE})"
fi

echo "==> verifying Nydus image exists in registry"
repo_tag="${NYDUS_IMAGE#*/}"
repo="${repo_tag%%:*}"
tag="${NYDUS_IMAGE##*:}"
if [[ "$repo" == *"amazonaws.com"* ]]; then
  aws ecr describe-images --repository-name "$repo" --image-ids imageTag="$tag" --region "$AWS_REGION" >/dev/null
else
  echo "skipping registry check for non-ECR image ${NYDUS_IMAGE}"
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
  --set nodeAgent.images="${FAKEVLLM_IMAGE},${NYDUS_IMAGE}" \
  --set spegel.enabled=false \
  --set garage.enabled=false \
  --set dcgmExporter.enabled=false \
  --wait --timeout 10m

kubectl apply -f "$ROOT/test/e2e/fixtures/gpu-smoke.yaml"

echo "==> warming base image via kubelet pull (ECR auth)"
kubectl delete pod nydus-warmup -n default --ignore-not-found --wait=true 2>/dev/null || true
kubectl run nydus-warmup -n default --restart=Never --image="$FAKEVLLM_IMAGE" \
  --overrides='{"spec":{"nodeSelector":{"opencoda.dev/gpu":"true"},"tolerations":[{"key":"opencoda.io/gpu","operator":"Exists","effect":"NoSchedule"}],"containers":[{"name":"nydus-warmup","image":"'"$FAKEVLLM_IMAGE"'","command":["sleep","3600"]}]}}'
for _ in $(seq 1 60); do
  phase="$(kubectl get pod nydus-warmup -n default -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  if [[ "$phase" == "Running" || "$phase" == "Succeeded" ]]; then
    break
  fi
  sleep 5
done
kubectl get pod nydus-warmup -n default -o wide

kubectl -n opencoda-system rollout restart daemonset/coda-node-agent
kubectl -n opencoda-system rollout status daemonset/coda-node-agent --timeout=300s

logs="$(kubectl -n opencoda-system logs daemonset/coda-node-agent --tail=200)"
echo "$logs"

if ! echo "$logs" | grep -q "cachefill: pulled ${FAKEVLLM_IMAGE}\|cachefill: pulled.*coda-fakevllm.*latest"; then
  echo "expected cachefill pull log for base image ${FAKEVLLM_IMAGE}" >&2
  exit 1
fi
if ! echo "$logs" | grep -q "cachefill: nydus OCI ${NYDUS_IMAGE} registered"; then
  echo "expected cachefill nydus registration log for ${NYDUS_IMAGE}" >&2
  exit 1
fi
if ! echo "$logs" | grep -q "cachefill: nydus prefetch ${NYDUS_IMAGE} ok"; then
  echo "expected cachefill nydus prefetch ok for ${NYDUS_IMAGE}" >&2
  exit 1
fi

echo "Nydus cachefill validation passed"
