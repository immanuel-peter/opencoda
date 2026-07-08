#!/usr/bin/env bash
# Shared gateway traffic helpers for OpenCoda e2e scripts.
set -euo pipefail

: "${CODA_NAMESPACE:=opencoda-system}"
: "${CODA_ENDPOINT_NS:=default}"
: "${CODA_ENDPOINT_NAME:=demo-vllm}"
: "${CODA_GATEWAY_PORT:=18090}"
: "${CODA_TEST_TOKEN_ID:=e2e-test-token}"
: "${CODA_TEST_TOKEN_SECRET:=e2e-test-secret}"
: "${CODA_TEST_MODEL:=hf://TinyLlama/TinyLlama-1.1B-Chat-v1.0}"

_e2e_gateway_pf_pid=""

e2e_token_hash() {
  local secret="$1"
  printf '%s' "$secret" | shasum -a 256 | awk '{print $1}'
}

e2e_create_token() {
  local ns="${1:-$CODA_ENDPOINT_NS}"
  local token_id="${2:-$CODA_TEST_TOKEN_ID}"
  local secret="${3:-$CODA_TEST_TOKEN_SECRET}"
  local hash
  hash="$(e2e_token_hash "$secret")"
  cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: opencoda.dev/v1alpha1
kind: CodaToken
metadata:
  name: ${token_id}
  namespace: ${ns}
spec:
  tokenID: ${token_id}
  secretHash: ${hash}
EOF
  echo "${token_id}:${secret}"
}

e2e_start_gateway_port_forward() {
  if [[ -n "${_e2e_gateway_pf_pid}" ]] && kill -0 "${_e2e_gateway_pf_pid}" 2>/dev/null; then
    return 0
  fi
  kubectl -n "$CODA_NAMESPACE" port-forward "svc/coda-gateway" "${CODA_GATEWAY_PORT}:8090" >/dev/null 2>&1 &
  _e2e_gateway_pf_pid=$!
  for _ in $(seq 1 30); do
    if curl -sf "http://127.0.0.1:${CODA_GATEWAY_PORT}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "gateway port-forward did not become ready" >&2
  return 1
}

e2e_stop_gateway_port_forward() {
  if [[ -n "${_e2e_gateway_pf_pid}" ]] && kill -0 "${_e2e_gateway_pf_pid}" 2>/dev/null; then
    kill "${_e2e_gateway_pf_pid}" 2>/dev/null || true
    wait "${_e2e_gateway_pf_pid}" 2>/dev/null || true
  fi
  _e2e_gateway_pf_pid=""
}

e2e_gateway_chat() {
  local token="$1"
  local model="${2:-$CODA_TEST_MODEL}"
  local outfile="${3:-}"
  local args=(
    -sS -o /dev/null -w "%{http_code}"
    -X POST "http://127.0.0.1:${CODA_GATEWAY_PORT}/v1/chat/completions"
    -H "Authorization: Bearer ${token}"
    -H "Content-Type: application/json"
    -d "{\"model\":\"${model}\",\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}"
  )
  if [[ -n "$outfile" ]]; then
    args=(-sS -o "$outfile" -w "%{http_code}" "${args[@]:2}")
  fi
  curl "${args[@]}"
}

e2e_gateway_chat_headers() {
  local token="$1"
  local model="${2:-$CODA_TEST_MODEL}"
  curl -sS -D - -o /dev/null \
    -X POST "http://127.0.0.1:${CODA_GATEWAY_PORT}/v1/chat/completions" \
    -H "Authorization: Bearer ${token}" \
    -H "Content-Type: application/json" \
    -d "{\"model\":\"${model}\",\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}"
}

e2e_wait_gateway_success() {
  local token="$1"
  local model="${2:-$CODA_TEST_MODEL}"
  local timeout="${3:-300}"
  local start
  start="$(date +%s)"
  while true; do
    code="$(e2e_gateway_chat "$token" "$model" || true)"
    if [[ "$code" == "200" ]]; then
      echo "$(( $(date +%s) - start ))"
      return 0
    fi
    if (( $(date +%s) - start > timeout )); then
      echo "timed out waiting for gateway 200 (last code=${code})" >&2
      return 1
    fi
    sleep 2
  done
}

e2e_patch_desired_replicas() {
  local desired="$1"
  kubectl -n "$CODA_ENDPOINT_NS" annotate "codaendpoint/${CODA_ENDPOINT_NAME}" \
    "opencoda.dev/desired-replicas=${desired}" --overwrite
}

e2e_delete_endpoint_pods() {
  kubectl -n "$CODA_ENDPOINT_NS" delete pod -l "opencoda.dev/endpoint=${CODA_ENDPOINT_NAME}" --ignore-not-found --wait=true
}

e2e_run_gateway_smoke() {
  local token
  token="$(e2e_create_token)"
  e2e_start_gateway_port_forward
  trap e2e_stop_gateway_port_forward EXIT

  echo "==> ensure endpoint has a replica"
  for _ in $(seq 1 90); do
    e2e_patch_desired_replicas 1
    if kubectl get pod -l "opencoda.dev/endpoint=${CODA_ENDPOINT_NAME}" -n "$CODA_ENDPOINT_NS" --no-headers 2>/dev/null | grep -q ' Running '; then
      if kubectl wait --for=condition=Ready pod -l "opencoda.dev/endpoint=${CODA_ENDPOINT_NAME}" -n "$CODA_ENDPOINT_NS" --timeout=30s 2>/dev/null; then
        break
      fi
    fi
    kubectl uncordon "$(kubectl get nodes -l opencoda.dev/gpu=true -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)" 2>/dev/null || true
    sleep 2
  done

  echo "==> gateway chat (expect 200)"
  code=""
  for _ in $(seq 1 30); do
    code="$(e2e_gateway_chat "$token")"
    if [[ "$code" == "200" ]]; then
      break
    fi
    sleep 2
  done
  if [[ "$code" != "200" ]]; then
    echo "expected HTTP 200, got ${code}" >&2
    return 1
  fi

  echo "==> scale-to-zero leg"
  e2e_patch_desired_replicas 0
  e2e_delete_endpoint_pods
  sleep 12

  headers="$(e2e_gateway_chat_headers "$token")"
  if ! echo "$headers" | grep -qi "HTTP/.* 429"; then
    echo "expected HTTP 429 during scale-to-zero, got:" >&2
    echo "$headers" >&2
    return 1
  fi
  if ! echo "$headers" | grep -qi "Retry-After:"; then
    echo "expected Retry-After header" >&2
    return 1
  fi

  e2e_patch_desired_replicas 1
  cold_secs="$(e2e_wait_gateway_success "$token")"
  echo "cold-start wall time (429->200): ${cold_secs}s"
  export CODA_COLD_START_WALL_SECS="$cold_secs"
  echo "gateway traffic smoke passed"
}
