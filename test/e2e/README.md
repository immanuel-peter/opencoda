# OpenCoda E2E validation

## Quick reference

| Job | Local | Crabbox | GHA |
|-----|-------|---------|-----|
| Unit + build | `make all` | `crabbox job run unit` | `ci.yml` → `unit` |
| Vet | `go vet ./...` | `crabbox job run vet` | `ci.yml` → `vet` |
| Kind E2E | `make e2e-kind` | `crabbox job run e2e-kind` | `ci.yml` → `e2e-kind` (main only) |
| EKS static | `make e2e-eks` | `crabbox job run e2e-eks` | manual / Crabbox |
| EKS GPU gate | `make e2e-eks-gpu` | manual | manual |
| EKS real vLLM wedge | `make e2e-eks-vllm` | manual | manual |
| UC1 bursty demo | `make e2e-uc1` | manual | manual |
| EKS spot | `make e2e-aws` | `crabbox job run e2e-eks-spot` | manual / Crabbox |
| Nydus cachefill | `make e2e-nydus` | manual | manual |
| Phase 1 sign-off | `hack/e2e-phase1-signoff.sh` | manual | manual |

## Phase 1 exit sign-off (July 2026)

After `aws login` and with `EKS_CLUSTER_NAME` set in repo-root `.env`:

```bash
# Full orchestrated run
chmod +x hack/e2e-phase1-signoff.sh
./hack/e2e-phase1-signoff.sh
```

Individual legs:

| Leg | Command | Proves |
|-----|---------|--------|
| Gateway traffic + scale-from-zero | `make e2e-eks-gpu` | CodaToken auth, `/v1/chat/completions` curl, 429→200 cold path |
| UC1 bursty trace | `make e2e-uc1` | Agent-shaped prefix reuse, utilization/KV-hit summary |
| Real vLLM + LMCache + Garage | `make e2e-eks-vllm` | Qwen2.5-0.5B on g5, cold-start status, KV hit rate |
| Spot pool | `make e2e-aws` | AWS spot node join (`AWS_SPOT_SUBNETS` in `.env`) |
| Nydus cachefill | `make e2e-nydus` | node-agent pull/prefetch on GPU node |

GPU nodegroup: `config/eks/gpu-nodegroup.yaml` (promoted from `tmp/eks/`). The add script scales `gpu-static` to 1 if nodes are missing.

## Crabbox

Install Crabbox, then from the repo root:

```bash
crabbox doctor
crabbox job list
crabbox job run unit
crabbox job run e2e-kind
```

EKS jobs need AWS + cluster env (use a profile, not the repo):

```bash
# ~/.config/opencoda/e2e.env
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
AWS_REGION=us-east-1
EKS_CLUSTER_NAME=opencoda-dev
AWS_SPOT_SUBNETS=subnet-aaa,subnet-bbb
CODA_JOIN_TOKEN=...   # only for spot pool node join

crabbox job run e2e-eks \
  --env-from-profile ~/.config/opencoda/e2e.env \
  --allow-env AWS_ACCESS_KEY_ID \
  --allow-env AWS_SECRET_ACCESS_KEY \
  --allow-env AWS_REGION \
  --allow-env EKS_CLUSTER_NAME \
  --allow-env CODA_JOIN_TOKEN
```

Hydration runs `.github/workflows/crabbox-hydrate.yml` (Go, kind, helm, kubectl, awscli).

Reuse a warm box:

```bash
crabbox prewarm --provider aws --class standard
crabbox job run --id blue-lobster unit
crabbox stop blue-lobster
```

## GitHub Actions

`/.github/workflows/ci.yml` mirrors `unit`, `vet`, and `e2e-kind` on hosted runners.

EKS/spot validation is intentionally Crabbox- or manual-driven (needs cloud creds and a cluster).

## Fixtures

- `fixtures/minimal.yaml` — static pool + demo endpoint (kind or EKS with pre-labeled GPU nodes)
- `fixtures/gpu-smoke.yaml` — EKS GPU gate: fakevllm, `minReplicas: 0`, scale-from-zero enabled
- `fixtures/gpu-vllm.yaml` — real vLLM (Qwen2.5-0.5B) + LMCache + Garage remote tier
- `fixtures/aws-spot-pool.yaml` — AWS spot GPUPool + buffer policy (`AWS_SPOT_SUBNETS` substituted by `hack/e2e-aws.sh`)

## Scripts

- `hack/e2e-kind.sh` — kind cluster + helm + smoke test
- `hack/e2e-eks.sh` — helm install on existing EKS (`--spot` for AWS pool)
- `hack/e2e-eks-gpu.sh` — GPU nodegroup + gateway traffic smoke
- `hack/e2e-eks-vllm.sh` — real vLLM wedge + Garage bootstrap
- `hack/e2e-uc1.sh` — UC1 loadgen against gateway
- `hack/e2e-aws.sh` — live spot pool validation
- `hack/e2e-nydus.sh` — Nydus convert + node-agent cachefill
- `hack/lib/e2e-gateway.sh` — shared CodaToken + curl helpers
- `hack/lib/garage-bootstrap.sh` — Garage bucket + S3 credentials
- `hack/ci-deps.sh` — installs kind/helm/kubectl/awscli (GHA + Crabbox hydrate)
