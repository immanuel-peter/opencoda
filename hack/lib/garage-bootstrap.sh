#!/usr/bin/env bash
# Bootstrap Garage bucket + S3 credentials for LMCache remote tier on EKS.
set -euo pipefail

: "${CODA_NAMESPACE:=opencoda-system}"
: "${GARAGE_BUCKET:=coda-kv}"
: "${GARAGE_ACCESS_KEY:=opencoda}"
: "${GARAGE_SECRET_KEY:=opencoda-garage-secret}"
: "${GARAGE_KEY_NAME:=lmcache}"

echo "==> bootstrapping Garage bucket ${GARAGE_BUCKET}"

kubectl -n "$CODA_NAMESPACE" wait --for=condition=available deployment/garage --timeout=300s

GARAGE_POD="$(kubectl -n "$CODA_NAMESPACE" get pod -l app=garage -o jsonpath='{.items[0].metadata.name}')"

kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage status >/dev/null 2>&1 || true

# Layout + bucket (idempotent best-effort; garage image has no shell).
kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage layout show 2>/dev/null | grep -q "z0" \
  || kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage layout assign -z z0 -c 1G 2>/dev/null || true
kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage layout apply --version 1 2>/dev/null || true
kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage bucket list 2>/dev/null | grep -q "${GARAGE_BUCKET}" \
  || kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage bucket create "${GARAGE_BUCKET}" 2>/dev/null || true

# Create or reuse S3 key for LMCache.
KEY_INFO="$(kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage key list 2>/dev/null | grep "${GARAGE_KEY_NAME}" || true)"
if [[ -z "$KEY_INFO" ]]; then
  kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage key create "${GARAGE_KEY_NAME}" 2>/dev/null || true
fi

KEY_ID="$(kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage key info "${GARAGE_KEY_NAME}" 2>/dev/null | awk '/Key ID/ {print $3}' || true)"
SECRET_KEY="$(kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage key info "${GARAGE_KEY_NAME}" 2>/dev/null | awk '/Secret key/ {print $3}' || true)"

if [[ -z "$KEY_ID" || -z "$SECRET_KEY" ]]; then
  KEY_ID="${GARAGE_ACCESS_KEY}"
  SECRET_KEY="${GARAGE_SECRET_KEY}"
fi

for ns in "$CODA_NAMESPACE" default; do
  kubectl -n "$ns" create secret generic garage-s3-credentials \
    --from-literal=AWS_ACCESS_KEY_ID="${KEY_ID}" \
    --from-literal=AWS_SECRET_ACCESS_KEY="${SECRET_KEY}" \
    --dry-run=client -o yaml | kubectl apply -f -
done

kubectl -n "$CODA_NAMESPACE" exec "$GARAGE_POD" -- /garage bucket allow \
  --read --write --owner "${GARAGE_KEY_NAME}" "${GARAGE_BUCKET}" 2>/dev/null || true

echo "Garage bootstrap complete (bucket=${GARAGE_BUCKET})"
