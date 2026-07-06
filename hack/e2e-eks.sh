#!/usr/bin/env bash
# Drive OpenCoda against an existing EKS cluster (Crabbox runner or local shell).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

: "${AWS_REGION:=us-east-1}"
: "${EKS_CLUSTER_NAME:?set EKS_CLUSTER_NAME}"
: "${CODA_NAMESPACE:=opencoda-system}"
: "${CODA_POOL:=onprem-static}"
: "${CODA_FAKE_HEALTH:=1}"
: "${HELM_RELEASE:=opencoda}"
: "${BUILD_IMAGES:=0}"

SPOT_MODE=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --spot)
      SPOT_MODE=1
      CODA_POOL=aws-spot
      shift
      ;;
    --build-images)
      BUILD_IMAGES=1
      shift
      ;;
    *)
      echo "unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

echo "==> configuring kubectl for EKS cluster ${EKS_CLUSTER_NAME} (${AWS_REGION})"
aws eks update-kubeconfig --name "$EKS_CLUSTER_NAME" --region "$AWS_REGION"

CONTROLLER_IMAGE="${CONTROLLER_IMAGE:-ghcr.io/immanuel-peter/opencoda/coda-controller-manager:latest}"
GATEWAY_IMAGE="${GATEWAY_IMAGE:-ghcr.io/immanuel-peter/opencoda/coda-gateway:latest}"

if [[ "$BUILD_IMAGES" == "1" ]]; then
  echo "==> building and loading controller image locally (requires docker + cluster pull access)"
  docker build -t "$CONTROLLER_IMAGE" -f hack/Dockerfile.controller .
  GATEWAY_IMAGE="$CONTROLLER_IMAGE"
fi

echo "==> installing OpenCoda via Helm"
helm upgrade --install "$HELM_RELEASE" "$ROOT/charts/opencoda" \
  --namespace "$CODA_NAMESPACE" \
  --set controllerManager.image="$CONTROLLER_IMAGE" \
  --set gateway.image="$GATEWAY_IMAGE" \
  --wait --timeout 10m

echo "==> controller env for e2e"
kubectl -n "$CODA_NAMESPACE" set env deployment/coda-controller-manager \
  CODA_FAKE_HEALTH="$CODA_FAKE_HEALTH" >/dev/null
if [[ -n "${CODA_JOIN_TOKEN:-}" ]]; then
  kubectl -n "$CODA_NAMESPACE" set env deployment/coda-controller-manager \
    CODA_JOIN_TOKEN="$CODA_JOIN_TOKEN" >/dev/null
fi

echo "==> installing CRDs"
kubectl apply -f "$ROOT/config/crd/bases/"

echo "==> waiting for CRDs"
kubectl wait --for=condition=established crd/gpupools.opencoda.dev --timeout=180s
kubectl wait --for=condition=established crd/bufferpolicies.opencoda.dev --timeout=180s
kubectl wait --for=condition=established crd/codaendpoints.opencoda.dev --timeout=180s

echo "==> applying fixtures"
kubectl apply -f "$ROOT/test/e2e/fixtures/minimal.yaml"
if [[ "$SPOT_MODE" == "1" ]]; then
  kubectl apply -f "$ROOT/test/e2e/fixtures/aws-spot-pool.yaml"
fi

echo "==> waiting for control plane pods"
kubectl -n "$CODA_NAMESPACE" rollout status deployment/coda-controller-manager --timeout=300s
kubectl -n "$CODA_NAMESPACE" rollout status deployment/coda-gateway --timeout=300s

echo "==> buffer reconcile window (pool=${CODA_POOL})"
sleep 120

echo "==> cluster snapshot"
kubectl get gpupool,bufferpolicy,codaendpoint -A || true
kubectl -n "$CODA_NAMESPACE" get pods

GW_SVC="coda-gateway"
if kubectl -n "$CODA_NAMESPACE" get svc "$GW_SVC" >/dev/null 2>&1; then
  echo "==> gateway service:"
  kubectl -n "$CODA_NAMESPACE" get svc "$GW_SVC" -o wide
fi

echo ""
echo "Next manual checks:"
echo "  1. coda token new --namespace default"
echo "  2. kubectl -n $CODA_NAMESPACE port-forward svc/coda-gateway 8090:8090"
echo "  3. curl -H 'Authorization: Bearer <id:secret>' http://127.0.0.1:8090/v1/chat/completions ..."
echo "  4. kubectl -n $CODA_NAMESPACE logs deploy/coda-controller-manager --tail=200"
