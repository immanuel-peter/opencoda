# OpenCoda

Apache-2.0 control plane for serverless GPU inference on infrastructure you own.

## Quick start

```bash
make generate manifests build test
helm upgrade --install opencoda charts/opencoda -n opencoda-system --create-namespace
kubectl apply -f test/e2e/fixtures/minimal.yaml
```

## Binaries

- `coda-controller-manager` — buffer, endpoint, health controllers
- `coda-gateway` — router, autoscaler, multi-model front door
- `coda-webhook` — admission validation
- `coda-node-agent` — node cachefill (Phase 1 stub)
- `coda` — CLI (`deploy`, `logs`, `scale`, `token`, `image convert`)

## Studio

Next.js App Router dashboard in `studio/` (Tier 1: endpoints + live logs).
