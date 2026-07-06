#!/usr/bin/env bash
set -euo pipefail

# Live AWS spot validation on an existing EKS cluster.
# Requires: EKS_CLUSTER_NAME, AWS credentials, aws-credentials secret in cluster.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

: "${AWS_REGION:=us-east-1}"
: "${EKS_CLUSTER_NAME:?set EKS_CLUSTER_NAME}"

exec "$ROOT/hack/e2e-eks.sh" --spot "$@"
