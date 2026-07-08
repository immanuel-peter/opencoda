#!/usr/bin/env bash
# Discover EKS spot provisioning params and write a patched GPUPool fixture.
set -euo pipefail

: "${EKS_CLUSTER_NAME:?set EKS_CLUSTER_NAME}"
: "${AWS_REGION:=us-east-1}"
: "${AWS_SPOT_SUBNETS:?set AWS_SPOT_SUBNETS}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEMPLATE="${ROOT}/test/e2e/fixtures/aws-spot-pool.yaml"
OUT="${1:-}"

if [[ -z "$OUT" ]]; then
  OUT="$(mktemp)"
fi

NODEGROUP="${AWS_SPOT_NODEGROUP:-gpu-static}"
ROLE_ARN="$(aws eks describe-nodegroup \
  --cluster-name "$EKS_CLUSTER_NAME" \
  --nodegroup-name "$NODEGROUP" \
  --region "$AWS_REGION" \
  --query 'nodegroup.nodeRole' \
  --output text 2>/dev/null || true)"
if [[ -n "$ROLE_ARN" && "$ROLE_ARN" == arn:aws:iam::* ]]; then
  ROLE_NAME="$(basename "$ROLE_ARN")"
  PROFILE_NAME="$(aws iam list-instance-profiles-for-role \
    --role-name "$ROLE_NAME" \
    --query 'InstanceProfiles[0].InstanceProfileName' \
    --output text 2>/dev/null || true)"
fi
if [[ -z "${PROFILE_NAME:-}" || "$PROFILE_NAME" == "None" ]]; then
  PROFILE_NAME="${AWS_NODE_INSTANCE_PROFILE:-}"
fi
if [[ -z "$PROFILE_NAME" ]]; then
  echo "could not resolve node instance profile (set AWS_NODE_INSTANCE_PROFILE)" >&2
  exit 1
fi

CLUSTER_JSON="$(aws eks describe-cluster --name "$EKS_CLUSTER_NAME" --region "$AWS_REGION" --output json)"
CLUSTER_SG="$(echo "$CLUSTER_JSON" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['cluster']['resourcesVpcConfig']['clusterSecurityGroupId'])")"
EXTRA_SGS="$(echo "$CLUSTER_JSON" | python3 -c "import json,sys; d=json.load(sys.stdin); print(','.join(d['cluster']['resourcesVpcConfig'].get('securityGroupIds',[])))")"
if [[ -n "$EXTRA_SGS" ]]; then
  SECURITY_GROUPS="${CLUSTER_SG},${EXTRA_SGS}"
else
  SECURITY_GROUPS="$CLUSTER_SG"
fi

sed \
  -e "s|subnets: \"\"|subnets: \"${AWS_SPOT_SUBNETS}\"|" \
  -e "s|region: us-east-1|region: ${AWS_REGION}|" \
  -e "/capacityType: spot/a\\
      clusterName: \"${EKS_CLUSTER_NAME}\"\\
      nodeInstanceProfile: \"${PROFILE_NAME}\"\\
      securityGroupIds: \"${SECURITY_GROUPS}\"
" \
  "$TEMPLATE" >"$OUT"

echo "$OUT"
