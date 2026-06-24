#!/usr/bin/env bash
set -euo pipefail

# Scripted live AWS spot validation — run with AWS credentials and EKS cluster configured.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

: "${AWS_REGION:=us-east-1}"
: "${CODA_POOL:=aws-spot}"

kubectl apply -f "$ROOT/test/e2e/fixtures/minimal.yaml"
echo "Waiting for buffer provision on pool $CODA_POOL..."
sleep 120
echo "Measure cold start: curl gateway /v1/chat/completions with CodaToken"
echo "kubectl -n opencoda-system logs deploy/opencoda-gateway"
