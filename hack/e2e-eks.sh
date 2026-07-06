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
: "${GHCR_USER:=immanuel-peter}"
: "${CODA_ENGINE_IMAGE:-}"
: "${CODA_E2E_FIXTURE:=minimal.yaml}"

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
  echo "==> building controller image locally"
  docker build -t "$CONTROLLER_IMAGE" -f hack/Dockerfile.controller .
  echo "==> push ${CONTROLLER_IMAGE} before running on EKS (or make GHCR packages public)"
fi

ensure_ghcr_pull_secret() {
  local token="${GHCR_TOKEN:-${GITHUB_TOKEN:-}}"
  if [[ -z "$token" ]] && command -v gh >/dev/null 2>&1; then
    token="$(gh auth token 2>/dev/null || true)"
  fi
  if [[ -z "$token" ]]; then
    echo "==> no GHCR_TOKEN set; assuming GHCR packages are public"
    return 0
  fi
  echo "==> configuring GHCR pull secret in ${CODA_NAMESPACE}"
  kubectl create namespace "$CODA_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret docker-registry ghcr-credentials \
    --docker-server=ghcr.io \
    --docker-username="$GHCR_USER" \
    --docker-password="$token" \
    -n "$CODA_NAMESPACE" \
    --dry-run=client -o yaml | kubectl apply -f -
}

ensure_ghcr_pull_secret

HELM_SET=(
  --set controllerManager.image="$CONTROLLER_IMAGE"
  --set gateway.image="$GATEWAY_IMAGE"
  --set controllerManager.fakeHealth="$CODA_FAKE_HEALTH"
  --set garage.enabled=false
  --set spegel.enabled=false
  --set studio.enabled=false
  --set nodeAgent.enabled=false
)
if [[ -n "$CODA_ENGINE_IMAGE" ]]; then
  HELM_SET+=(--set controllerManager.engineImage="$CODA_ENGINE_IMAGE")
fi
if kubectl -n "$CODA_NAMESPACE" get secret ghcr-credentials >/dev/null 2>&1; then
  HELM_SET+=(--set-json 'imagePullSecrets=[{"name":"ghcr-credentials"}]')
fi

echo "==> installing CRDs"
kubectl apply -f "$ROOT/config/crd/bases/"
kubectl wait --for=condition=established crd/gpupools.opencoda.dev --timeout=180s
kubectl wait --for=condition=established crd/bufferpolicies.opencoda.dev --timeout=180s
kubectl wait --for=condition=established crd/codaendpoints.opencoda.dev --timeout=180s

echo "==> installing OpenCoda via Helm"
helm upgrade --install "$HELM_RELEASE" "$ROOT/charts/opencoda" \
  --namespace "$CODA_NAMESPACE" --create-namespace \
  "${HELM_SET[@]}" \
  --wait --timeout 10m

# Recycle pods so new pulls pick up ghcr-credentials (Helm --wait can stall on pre-secret pods).
kubectl -n "$CODA_NAMESPACE" rollout restart deployment/coda-controller-manager deployment/coda-gateway
kubectl -n "$CODA_NAMESPACE" rollout status deployment/coda-controller-manager --timeout=300s
kubectl -n "$CODA_NAMESPACE" rollout status deployment/coda-gateway --timeout=300s

echo "==> applying fixtures (${CODA_E2E_FIXTURE})"
kubectl apply -f "$ROOT/test/e2e/fixtures/${CODA_E2E_FIXTURE}"
if [[ "$SPOT_MODE" == "1" ]]; then
  kubectl apply -f "$ROOT/test/e2e/fixtures/aws-spot-pool.yaml"
fi

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
