#!/usr/bin/env bash
# Phase 1 exit validation orchestrator (run after aws login + GPU nodegroup scaled).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "==> 1/5 GPU smoke + gateway traffic"
make e2e-eks-gpu

echo "==> 2/5 UC1 bursty agent trace"
make e2e-uc1

echo "==> 3/5 real vLLM + LMCache + Garage"
make e2e-eks-vllm
chmod +x hack/lib/lmcache-tier-spill.sh
hack/lib/lmcache-tier-spill.sh

echo "==> 4/5 spot pool (requires AWS_SPOT_SUBNETS in .env)"
make e2e-aws

echo "==> 5/5 Nydus cachefill"
make e2e-nydus

echo "Phase 1 exit validation complete — capture metrics for PRD §26 sign-off"
