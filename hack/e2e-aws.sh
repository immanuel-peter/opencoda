#!/usr/bin/env bash
# Live AWS spot pool validation on an existing EKS cluster.
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
: "${AWS_SPOT_SUBNETS:?set AWS_SPOT_SUBNETS to comma-separated subnet IDs}"

CODA_FAKE_HEALTH="${CODA_FAKE_HEALTH:-0}"
export CODA_FAKE_HEALTH

echo "==> ensuring in-cluster aws-credentials secret"
if [[ -n "${AWS_ACCESS_KEY_ID:-}" && -n "${AWS_SECRET_ACCESS_KEY:-}" ]]; then
  kubectl create namespace opencoda-system --dry-run=client -o yaml | kubectl apply -f -
  kubectl -n opencoda-system create secret generic aws-credentials \
    --from-literal=AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID}" \
    --from-literal=AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY}" \
    --dry-run=client -o yaml | kubectl apply -f -
else
  echo "AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY required in .env for spot provisioning" >&2
  exit 2
fi

SPOT_FIXTURE="$(mktemp)"
trap 'rm -f "$SPOT_FIXTURE"' EXIT
chmod +x hack/lib/aws-spot-bootstrap.sh
SPOT_FIXTURE="$(./hack/lib/aws-spot-bootstrap.sh "$SPOT_FIXTURE")"

chmod +x hack/e2e-eks.sh
export CODA_SPOT_FIXTURE="$SPOT_FIXTURE"
CODA_E2E_FIXTURE=minimal.yaml CODA_GATEWAY_TEST=0 ./hack/e2e-eks.sh --spot

echo "==> scaling aws-spot buffer to request a warm GPU"
kubectl patch bufferpolicy aws-spot --type=merge -p '{"spec":{"target":{"minWarmGPUs":1,"maxWarmGPUs":1}}}'

echo "==> waiting for spot pool node join"
for _ in $(seq 1 60); do
  buffered="$(kubectl get gpupool aws-spot -o jsonpath='{.status.nodes.buffered}' 2>/dev/null || echo 0)"
  active="$(kubectl get gpupool aws-spot -o jsonpath='{.status.nodes.active}' 2>/dev/null || echo 0)"
  joined="$(kubectl get nodes -l opencoda.dev/pool=aws-spot --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  total=$((buffered + active))
  if [[ "${total:-0}" -ge 1 || "${joined:-0}" -ge 1 ]]; then
    echo "spot pool ready (buffered=${buffered} active=${active} joined_nodes=${joined})"
    kubectl get nodes -L opencoda.dev/pool,opencoda.dev/gpu
    echo "spot validation passed"
    exit 0
  fi
  sleep 20
done

echo "spot pool did not register a node within timeout" >&2
kubectl -n opencoda-system logs deploy/coda-controller-manager --tail=200 || true
exit 1
