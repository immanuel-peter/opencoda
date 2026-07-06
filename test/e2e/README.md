# OpenCoda E2E validation

## Quick reference

| Job | Local | Crabbox | GHA |
|-----|-------|---------|-----|
| Unit + build | `make all` | `crabbox job run unit` | `ci.yml` → `unit` |
| Vet | `go vet ./...` | `crabbox job run vet` | `ci.yml` → `vet` |
| Kind E2E | `make e2e-kind` | `crabbox job run e2e-kind` | `ci.yml` → `e2e-kind` (main only) |
| EKS static | `make e2e-eks` | `crabbox job run e2e-eks` | manual / Crabbox |
| EKS spot | `make e2e-aws` | `crabbox job run e2e-eks-spot` | manual / Crabbox |

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
- `fixtures/aws-spot-pool.yaml` — AWS spot GPUPool + buffer policy (edit subnet IDs first)

## Scripts

- `hack/e2e-kind.sh` — kind cluster + helm + smoke test
- `hack/e2e-eks.sh` — helm install on existing EKS (`--spot` for AWS pool)
- `hack/e2e-aws.sh` — wrapper for `e2e-eks.sh --spot`
- `hack/ci-deps.sh` — installs kind/helm/kubectl/awscli (GHA + Crabbox hydrate)
