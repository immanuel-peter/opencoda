#!/usr/bin/env bash
# Prove LMCache remote tier spill: restart replica and check KV hit rate.
set -euo pipefail

: "${CODA_ENDPOINT_NS:=default}"
: "${CODA_ENDPOINT_NAME:=demo-vllm}"

echo "==> deleting endpoint pods to force cold restart"
kubectl -n "$CODA_ENDPOINT_NS" annotate "codaendpoint/${CODA_ENDPOINT_NAME}" \
  opencoda.dev/desired-replicas=1 --overwrite
kubectl -n "$CODA_ENDPOINT_NS" delete pod -l "opencoda.dev/endpoint=${CODA_ENDPOINT_NAME}" --wait=true

echo "==> waiting for replacement pod"
for _ in $(seq 1 40); do
  kubectl uncordon "$(kubectl get nodes -l opencoda.dev/gpu=true -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)" 2>/dev/null || true
  if kubectl wait --for=condition=Ready pod -l "opencoda.dev/endpoint=${CODA_ENDPOINT_NAME}" -n "$CODA_ENDPOINT_NS" --timeout=15s 2>/dev/null; then
    break
  fi
  sleep 15
done

echo "==> endpoint status after restart"
kubectl get codaendpoint "$CODA_ENDPOINT_NAME" -n "$CODA_ENDPOINT_NS" -o jsonpath='{.status.kvHitRate}{"\n"}{.status.coldStart.p50ms}{"\n"}' || true
