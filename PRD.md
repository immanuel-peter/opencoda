# OpenCoda — Joint PRD & Engineering Design Document

**Status:** Draft v1.4
**Author:** Immanuel Peter
**Date:** June 2026
**Audience:** Tensormesh engineering & leadership, prospective OSS contributors, design partners

---

# Part I — Product Requirements Document

## 1. One-liner

OpenCoda is an open-source control plane for serverless GPU inference on infrastructure **you** own. It brings Modal-class cold starts (tens of seconds, not kiloseconds) and scale-to-zero economics to self-hosted, cross-cloud GPU fleets — with persistent KV cache (LMCache) as a first-class primitive, so replicas wake up *warm*, not just *fast*.

## 2. Problem statement

Enterprises self-hosting LLMs face a structural utilization trap:

- **Inference demand is spiky.** Coding agents, automation, and internal tools produce daytime-heavy, bursty traffic with peak-to-average ratios of 5–10x. Capacity is sized for the peak; the average pays for it.
- **GPU Allocation Utilization is dismal.** Industry data (State of AI Infrastructure at Scale, 2024) shows most orgs achieve <70% allocation utilization *at peak*; real-world figures commonly land at 10–20%. Effective cost per token is 5–10x the spec-sheet number.
- **Naïve autoscaling doesn't work for GPUs.** Spinning a fresh inference replica — instance allocation, image pull, engine init, weight load, CUDA graph capture — takes minutes to tens of minutes. By the time capacity arrives, the spike is gone and QoS already degraded.
- **The proprietary platforms solved this, but only on their fleet.** Modal's published architecture (buffer + lazy filesystem + CPU/GPU checkpoint-restore) achieves ~40x faster replica spin-up (≈2,000s → ≈50s). None of it is consumable on your own clusters, your own clouds, your own committed-use contracts, or inside your own VPC.
- **Serverless breaks agent workloads.** Aggressive scale-to-zero means every fresh replica starts with an empty KV cache. Agent traffic re-sends 50–100k-token contexts per turn with 80–95% prefix overlap; cold-cache re-prefill is exactly where agent GPU-seconds go. Modal explicitly treats KV cache as disposable on restore. Serverless and agents are in tension — unless the KV cache is a persistent, shared resource.

**No open-source project occupies this intersection.** AIBrix, KServe, and Knative do not own a KV persistence layer; Modal cannot ship one as a differentiator; SkyPilot is a job orchestrator, not a long-lived control plane.

## 3. Goals

1. **G1 — Cold start:** New inference replica serving traffic in ≤60s (Phase 1) and ≤20s (Phase 3) for a 1–10 GiB model, measured from scale-up decision to first successful request.
2. **G2 — Utilization:** Enable customers to run at ≥60% GPU Allocation Utilization on bursty workloads without QoS degradation (p99 TTFT within 2x of warm-replica baseline during 5x demand spikes).
3. **G3 — Cross-cloud abstraction:** A user defines GPU capacity once via `GPUPool` resources; the buffer controller arbitrages across pools (multi-cloud, spot, on-prem) without app-level changes.
4. **G4 — Warm restore:** Restored/new replicas attach to a shared LMCache pool; first-request prefix hit rate ≥80% on agent-shaped traffic.
5. **G5 — Adoptability:** Single Helm install onto any conformant Kubernetes ≥1.30 cluster; zero application code changes for vLLM workloads in v1 (SGLang via the same `Engine` interface post-v1).

## 4. Non-goals (v1)

- **Training or fine-tuning orchestration.** Inference only.
- **Multi-GPU (TP/PP) snapshot-restore.** NCCL collectives deadlock under pause; single-GPU snapshots only until coordinated checkpoint lands (§17.6). Multi-GPU *serving* is fully supported in v1 — buffer, lazy pulls, autoscaling, and LMCache warm attach all apply; only the snapshot path is skipped. Cold start for large TP models is weight-load-bound anyway, where snapshots help least.
- **A hosted/managed service.** That is the TensorCoda commercial layer (§9), out of scope for OSS v1.
- **Building a custom filesystem.** We compose containerd snapshotters (Nydus/SOCI); we do not write FUSE code.
- **An LP/MIP scheduler in v1.** Greedy price-priority fill first; solver behind an interface in Phase 4.
- **RDMA weight serving.** Acknowledged bottleneck; exposed as an extension point (`WeightSource`), not built.
- **SGLang engine in v1.** v1 ships **vLLM only** behind a pluggable `Engine` interface (`pkg/engine`); admission rejects non-`vllm` `engine.type`. SGLang slots in as a second `Engine` implementation without API churn; `sglang-router` adoption remains Phase 3 (FR-14b).

## 5. Users & personas

| Persona | Need | Success looks like |
|---|---|---|
| **Platform/Infra engineer** (primary) | Run self-hosted LLM endpoints for internal teams without owning a 24/7 idle fleet | Helm install, write 3 CRs, GPU bill drops 50–70% |
| **ML engineer** | Deploy a model endpoint that autoscales, without learning K8s internals | `coda deploy` via Python SDK; endpoint URL back in minutes |
| **VP Infrastructure** (economic buyer) | Cut the GPU line item; keep code/data in VPC | Savings dashboard showing allocation utilization and $ saved |
| **OSS contributor** | Add a capacity provider for their cloud | Implement one Go interface, pass conformance suite |

## 6. Core use cases

- **UC1 — Internal coding-agent fleet.** 2,000-engineer org runs agents against a self-hosted 70B-class model. Traffic: near-zero nights/weekends, 10am–6pm peaks, fan-out bursts (one agent → 40 parallel tool loops). OpenCoda scales replicas with the curve; LMCache makes turn-N prefill a ~95% cache hit.
- **UC2 — Bursty batch extraction.** Document-processing jobs arrive unpredictably with tens-of-minutes deadlines, requiring 100–1,000 GPU surges. Buffer absorbs the leading edge; fast cold starts fill the rest; everything releases on completion.
- **UC3 — Multi-cloud cost arbitrage.** Org holds AWS committed-use + GCP spot + a bare-metal cluster. Buffer controller fills from cheapest observed (not advertised) capacity, respecting per-pool ceilings.
- **UC4 — Many-model long tail.** Dozens of internal endpoints, each low-traffic. Scale-to-zero per endpoint + shared warm node buffer + content-addressed image cache make the long tail nearly free.

## 7. Product requirements

### P0 (Phase 1 — "demoable wedge")
- FR-1: `GPUPool`, `BufferPolicy`, `CodaEndpoint` CRDs with validation and status conditions.
- FR-1a: Endpoint lifecycle: surge-through-buffer rolling upgrades on `engine.version`/`model.source` change; snapshot-key-driven invalidation and re-bake (§13.5).
- FR-2: Buffer controller maintaining N warm GPU nodes across pools; greedy price-priority fill; scale-down with hysteresis.
- FR-3: In-tree capacity providers: AWS, GCP, Static (on-prem/byo-node).
- FR-4: Lazy image pulls via Nydus snapshotter; zstd image conversion tooling in CI (`coda image convert`).
- FR-4a: Curated engine image matrix (engine × version × CUDA × LMCache connector, pre-Nydus-converted, snapshot-validated) with per-image **prefetch manifests** traced at publish time (§16).
- FR-5: LMCache wired by default into vLLM pods: CPU-RAM + NVMe + object-storage tiers; shared pool per endpoint.
- FR-5b: `Engine` plugin interface (§14.3) with `vllm` as the sole v1 implementation; `RenderPodSpec` composes with `KVProvider` pod patches.
- FR-5a: `KVProvider` plugin interface (§14.2) with capability flags; LMCache as reference/default implementation, `null` provider for testing.
- FR-6: Request router + autoscaler (scale 0→N on queue depth/concurrency; KV-affinity-aware routing, gated on provider `AffinityHints` capability). v1 router is the thin Go proxy behind a pluggable `Router` boundary (§18); engine-native KV-aware routers adopted in Phase 3 (FR-14b).
- FR-6b: Optional multi-model front door — single OpenAI-compatible ingress dispatching on the request `model` field to the owning `CodaEndpoint` (UC4 long tail); endpoint remains the lifecycle/KV/cost unit (§18).
- FR-6a: `RuntimeClass` surfacing: `CodaEndpoint.spec.runtime.class` (runc default; gvisor opt-in for untrusted function workloads); admission webhook rejects `SnapshotClass` on non-runc runtimes.
- FR-7: GPU health controller: DCGM boot check, Xid watch → cordon/drain/release.
- FR-8: Python SDK + CLI (`coda deploy`, `coda logs`, `coda scale`).
- FR-8a: Token auth: `CodaToken` CRD-backed tokens (`coda token new`), `CODA_SERVER_URL` + token-pair env/config convention, kubeconfig-RBAC bootstrap.
- FR-9: Metrics: allocation utilization, cold-start latency histogram, LMCache hit rate, per-pool $ estimate.
- FR-9a: Studio Tier 1: live endpoint/replica status list + per-call streaming logs (see §21). **Shipped:** Next.js 16+ App Router, pnpm, TypeScript, shadcn/ui + Tailwind (`studio/`).

### P1 (Phase 2 — CPU snapshots)
- FR-10: Node agent (DaemonSet) with custom runtime handler; CRIU checkpoint/restore of host-side engine state.
- FR-11: Snapshot cache keyed on (image digest, model ref, CPU feature set, kernel ABI, driver version); delivered through the same lazy/content-addressed path as images.
- FR-12: `SnapshotClass` CRD: what to checkpoint, when, retention.
- FR-12a: In-cluster image builder: BuildKit job consumes SDK `Image` definitions, emits Nydus-converted images (server-side builds, Modal-style UX).
- FR-12b: Modal compatibility shim (`coda.compat`): `App`/`@function`/`Image`/`@cls`/`.remote()` surface; documented gap list (Dict/Queue need optional NATS/Redis backing, Volume → PVC/S3 semantics, no Sandboxes).
- FR-12c: Studio Tier 2: replica lifecycle timeline (alloc → image → host-restore → device-restore → KV-attach → serving) with per-segment timings.
- FR-12d: Engine-grade autoscaling: scrape engine-native metrics (KV-cache utilization, pending prefill tokens, TTFT/TPOT) and accept per-endpoint latency SLOs driving scaling + load-shedding, replacing concurrency-only signals.
- FR-12e: Studio Fleet view: per-pool node/GPU inventory with lifecycle state, DCGM telemetry, Xid/health history, and replica placement (see §21).
- FR-12f: Studio History & events: filterable control-plane event feed (provision/release, ICE spill, cordon/drain, snapshot bake/invalidate, restore→cold-boot fallback, scale-to-zero) plus bounded history of completed jobs and replicas (status, timings, cold-start path, Loki-backed logs). Controllers emit K8s Events and mirror them as structured Loki lines; TTL'd record retention (days-to-weeks), no new datastore (see §21).

### P2 (Phase 3 — GPU snapshots)
- FR-13: cuda-checkpoint (CRIU CUDA plugin) integration; weight offload-to-host pre-checkpoint; KV cache excluded from snapshot, reattached via LMCache on restore ("warm restore").
- FR-14: Snapshot compatibility solver: restore only onto nodes whose key superset matches.
- FR-14a: Studio Tier 3: economics dashboard — live demand-vs-provisioned curve, allocation utilization, per-pool observed price/ICE history, LMCache hit rate & prefill GPU-seconds avoided, "$ saved vs. static provisioning" counter.
- FR-14b: Engine-native KV-aware router adoption behind the `Router` boundary: `vllm-router`/`sglang-router` consuming LMCache's KV-event stream for exact cache-aware dispatch, replacing the thin proxy for vLLM/SGLang endpoints; thin proxy remains fallback (§18). Resolves §27 open questions 2 & 7 toward LMCache-native fingerprints.

### P3 (Phase 4 — scale-out & ecosystem)
- FR-15: Pluggable `Scheduler` with OR-Tools min-cost implementation; observed-capacity feedback loop.
- FR-15a: Prefill/decode disaggregation (flag-gated experiment): heterogeneous replica roles per endpoint, role-aware gateway routing, LMCache as KV transfer fabric (§25). Rides the engine-native router's existing P/D support (`vllm-router` NIXL/NCCL/Mooncake connectors) behind the `Router` boundary (§18, FR-14b) rather than a bespoke role-router.
- FR-16: CapacityProvider conformance test suite + out-of-tree plugin loading (gRPC, hashicorp/go-plugin style).
- FR-17: Multi-cluster federation (commercial-adjacent; API stubs only in OSS).

## 8. Success metrics

| Metric | Phase 1 target | Phase 3 target |
|---|---|---|
| Cold start, 1 GiB model (p50 / p95) | 60s / 120s | 15s / 30s |
| Cold start, 8B-class model (p50) | 90s | 25s |
| First-request LMCache prefix hit rate (agent replay benchmark) | ≥70% | ≥85% |
| Allocation utilization on reference bursty trace | ≥45% | ≥65% |
| Buffer scheduling decision latency | <500ms | <500ms |
| Time-to-first-deploy for a new user | <30 min | <15 min |
| Community: GitHub stars / design partners | 1k / 2 | 5k / 5 |

## 9. Commercialization boundary (OpenCoda OSS vs. TensorCoda)

Recorded here so the OSS scope stays honest and the commercial line is drawn where enterprises feel pain, not arbitrarily.

**OSS (Apache-2.0):** everything in §7 FR-1…FR-14, single-cluster, full LMCache integration, greedy scheduler, in-tree providers, metrics.

**TensorCoda (commercial):** multi-cluster/multi-region federation, LP/MIP scheduler with global price feeds, RBAC/SSO/audit, savings & chargeback dashboards, snapshot fleet management at org scale, SLA support, managed control plane. Pricing model: % of GPU spend under management (Spot.io-shaped), value-anchored on the savings dashboard the OSS metrics already produce.

**Strategic note:** Modal architecturally treats KV cache as disposable on restore; AIBrix/KServe do not own the LMCache roadmap. The warm-restore primitive (FR-13) is the moat claim and must remain the most polished path in the product.

## 10. Competitive landscape

| | Cold-start tech | BYO cloud | KV persistence | OSS |
|---|---|---|---|---|
| **Modal** | Buffer + ImageFS + gVisor C/R + GPU C/R | ✗ (their fleet) | ✗ (explicitly disposable) | ✗ |
| **AIBrix** | Partial (engine-level) | ✓ (K8s) | Distributed KV experiments, no LMCache ownership | ✓ |
| **KServe/Knative** | Generic scale-to-zero, slow for GPUs | ✓ | ✗ | ✓ |
| **SkyPilot** | N/A (job launcher, not control plane) | ✓ | ✗ | ✓ |
| **OpenCoda** | Buffer + Nydus + CRIU + cuda-checkpoint | ✓ (CapacityProvider) | ✓ (LMCache first-class) | ✓ |

## 11. Assumptions & risks

- **A1:** NVIDIA driver r550+ available on target fleets (cuda-checkpoint dependency). *Mitigation:* snapshot path is optional per-endpoint; Phases 1–2 don't require it.
- **A2:** Customers can grant cloud IAM scoped to instance lifecycle. *Mitigation:* Static provider for orgs that pre-provision nodes themselves.
- **R1 — Scope risk:** seed-stage sponsor; control planes are big. *Mitigation:* phased plan front-loads the demoable, low-risk slice; GPU C/R (highest jank) ships last.
- **R2 — Ecosystem antagonism:** platforms embedding LMCache may view this as competitive. *Mitigation:* symmetric with absorption risk of doing nothing; LMCache remains independently consumable.
- **R3 — Snapshot fragility:** host heterogeneity breaks restores (e.g., missing CPU instructions → SIGILL). *Mitigation:* strict compatibility keying (FR-11/14); fall back to cold boot on key miss, never fail the request path.

---

# Part II — Engineering Design Document

## 12. Architecture overview

OpenCoda is a set of Kubernetes controllers (Go, controller-runtime) plus a per-node agent, layered on stock containerd. Nothing replaces the kubelet or scheduler; OpenCoda extends them via CRDs, a runtime handler, and a node-provisioning loop (Karpenter-shaped, not Cluster-API-shaped — node lifecycle is latency-critical and belongs to one tight loop).

```
                          ┌────────────────────────────────────────────────┐
                          │                CONTROL PLANE                   │
  coda CLI / Python SDK   │                                                │
        │                 │  ┌──────────────┐   ┌──────────────────────┐   │
        ▼                 │  │  API / CRDs  │   │   Buffer Controller  │   │
  ┌──────────────┐ gRPC   │  │ GPUPool      │◄──┤  reconcile:          │   │
  │ coda-gateway ├───────►│  │ BufferPolicy │   │  desired = active    │   │
  │ (router +    │        │  │ CodaEndpoint │   │          + buffer    │   │
  │  autoscaler) │        │  │ SnapshotClass│   │  fill via Scheduler  │   │
  └──────┬───────┘        │  └──────────────┘   └──────────┬───────────┘   │
         │                │  ┌──────────────┐              │ Quote/        │
         │                │  │ Health Ctrl  │              │ Provision     │
         │                │  │ (DCGM, Xid)  │   ┌──────────▼───────────┐   │
         │                │  └──────────────┘   │  CapacityProviders   │   │
         │                │                     │  aws | gcp | static  │   │
         │                └─────────────────────┴──────────┬───────────┘   │
         │                                                 │ nodes join    │
         ▼                                                 ▼               │
  ┌─────────────────────────────── GPU NODE ──────────────────────────────┐
  │  coda-node-agent (DaemonSet)                                          │
  │   ├─ runtime handler: cold-boot | criu-restore | cuda-restore         │
  │   ├─ snapshot manager (create/validate/key/GC)                        │
  │   └─ cache fill daemon (images, snapshots, weights → NVMe)            │
  │  containerd + nydus-snapshotter (lazy pulls)                          │
  │  ┌──────────────────────────────┐  ┌────────────────────────────┐    │
  │  │ vLLM / SGLang pod            │  │  LMCache local tiers        │    │
  │  │  + LMCache connector ────────┼──┤  GPU → CPU RAM → NVMe ──────┼──► │
  │  └──────────────────────────────┘  └────────────────────────────┘    │
  └───────────────────────────────────────────────────────────────────────┘
                                                  │
                              shared remote tier  ▼
                       ┌───────────────────────────────────┐
                       │ Object storage (S3/GCS/Garage)    │
                       │  • content-addressed image chunks │
                       │  • snapshot artifacts             │
                       │  • model weights                  │
                       │  • LMCache cold tier              │
                       └───────────────────────────────────┘
```

**Subsystems (8):** (1) CRD API layer, (2) Buffer Controller + Scheduler, (3) CapacityProvider plugins, (4) Image delivery, (5) Node agent + Snapshot manager, (6) LMCache integration, (7) Gateway (router/autoscaler), (8) Health & observability.

**Monorepo layout (implemented):** `github.com/immanuel-peter/opencoda` — Go module with `api/v1alpha1/` CRDs (`opencoda.dev/v1alpha1`), `cmd/{coda-controller-manager,coda-gateway,coda-node-agent,coda-webhook,coda}`, pluggable `pkg/{capacity,kv,engine,scheduler,weights}`, `internal/{controller,gateway,webhook,metrics,nodeagent}`, `sdk/python/`, `studio/` (Next.js), `charts/opencoda/`, `config/crd/bases/`, `test/e2e/`. Toolchain: Go 1.26, controller-runtime v0.24.1, K8s 1.36.

## 13. CRD specifications

### 13.1 `GPUPool` — a homogeneous source of capacity

One per (provider, region, instance family). Written once by the platform team.

```yaml
apiVersion: opencoda.dev/v1alpha1
kind: GPUPool
metadata:
  name: aws-use1-h100-spot
spec:
  provider:
    name: aws                     # matches a registered CapacityProvider
    credentialsRef: {secretName: aws-coda}
    params:
      region: us-east-1
      subnets: [subnet-abc]
      capacityType: spot          # spot | on-demand | reserved
  instanceTypes: [p5.48xlarge, p5e.48xlarge]
  gpu: {type: H100, perNode: 8}
  limits:
    maxNodes: 16
    maxHourlyUSD: 400             # hard spend ceiling, enforced pre-Provision
  priority: 10                    # lower = preferred by greedy scheduler
  taints: [{key: opencoda.dev/gpu, effect: NoSchedule}]
status:
  observedCapacity:               # fed back by provider; truth, not advertisement
    available: 6
    lastICE: "2026-06-08T14:11:00Z"   # last InsufficientCapacityError
    observedHourlyUSD: 21.4
  nodes: {active: 4, buffered: 1, provisioning: 0}
  conditions: [...]
```

### 13.2 `BufferPolicy` — the cost/latency dial

```yaml
apiVersion: opencoda.dev/v1alpha1
kind: BufferPolicy
metadata:
  name: default
spec:
  target:
    mode: dynamic                 # static | dynamic
    minWarmGPUs: 2
    maxWarmGPUs: 12
    dynamic:                      # buffer ∝ recent demand volatility
      window: 30m
      formula: "ceil(k * stddev(demand))"
      k: 1.5
  pools:                          # which pools may back the buffer, in order
    - name: onprem-static         # sunk cost first
    - name: aws-use1-h100-spot
    - name: gcp-usc1-h100-od
  scaleDown:
    stabilizationWindow: 10m      # hysteresis: don't churn nodes on noise
    drainTimeout: 5m
```

### 13.3 `CodaEndpoint` — the workload

```yaml
apiVersion: opencoda.dev/v1alpha1
kind: CodaEndpoint
metadata:
  name: qwen3-32b-agents
spec:
  model:
    source: hf://Qwen/Qwen3-32B   # or s3:// via WeightSource
    quantization: fp8
  engine:
    type: vllm                    # vllm only in v1 (admission rejects others); sglang post-v1 via Engine iface
    version: "0.11"
    args: ["--max-model-len", "131072"]
  resources: {gpu: 1, gpuType: H100}
  runtime:
    class: runc                   # runc (default, snapshot-capable) | gvisor (sandboxed, cold-boot only)
  scaling:
    minReplicas: 0
    maxReplicas: 32
    target: {metric: concurrency, value: 8}
    scaleToZeroAfter: 5m
  snapshot:
    classRef: gpu-warm-restore    # omit → cold boot path only
  kv:
    lmcache:
      enabled: true
      shared: true                # endpoint-wide shared pool
      tiers: [cpu, nvme, s3]
      remoteRef: {bucket: coda-kv, prefix: qwen3-32b/}
status:
  replicas: {ready: 3, starting: 1}
  coldStart: {p50ms: 18400, p95ms: 31000}
  kvHitRate: 0.87
```

### 13.4 `SnapshotClass`

```yaml
apiVersion: opencoda.dev/v1alpha1
kind: SnapshotClass
metadata:
  name: gpu-warm-restore
spec:
  scope: cpu+gpu                  # cpu | cpu+gpu
  checkpointAfter: readinessProbe # snapshot once engine reports ready
  pre:
    offloadWeightsToHost: true    # required for vLLM/SGLang GPU snapshots
    dropKVCache: true             # KV excluded; reattached via LMCache
  storage: {ref: s3://coda-snaps, compression: none}  # gzip is a 100MB/s trap
  retention: {maxPerKey: 2, ttl: 30d}
```

### 13.5 Endpoint lifecycle & engine management

`CodaEndpoint` is the *entire* management surface for engine fleets — teams never write Deployments, Services, HPAs, or launch scripts. The endpoint controller renders the full pod spec: engine container, flags, probes, metrics annotations, GPU resources, runtime class, and the `KVProvider` wiring. A minimal endpoint is `model` + `engine.type` + `resources`; everything else defaults from Helm-level platform config.

**Curated engine matrix.** OpenCoda publishes pre-built images per (engine × version × CUDA × LMCache connector), already Nydus-converted and snapshot-validated; `engine: {type: vllm, version: "0.11"}` resolves against the matrix. This is maintenance the project owns so users don't — it is exactly the configuration burden (image builds, CUDA/driver/connector pinning, launch args) that exists in raw-K8s land and disappears here. `engine.args` passes through validated extra flags; `engine.type: custom` + `podTemplate` is the escape hatch, with capability-degraded honesty: no snapshots without `SnapshotClass` hook cooperation, KV features only via a `KVProvider`-supported connector.

**Upgrades: surge through the buffer.** A one-line change to `engine.version` or `model.source` triggers a surge rollout sourced from buffered capacity — new-version replicas come up (fast; snapshot-restored once the new artifact bakes), take traffic, old replicas drain. No capacity dip, no downtime. No stale-snapshot footgun either: engine version and model ref are in the snapshot key (§17.1), so upgrades automatically miss old artifacts, cold-boot once per node class, re-checkpoint, and subsequent replicas restore warm. Invalidation falls out of the keying for free.

**Day-2 defaults.** Engine `/metrics` (KV utilization, TTFT, queue depths) auto-scraped into the same Prometheus feeding Studio and the autoscaler; logs via `coda logs`/Studio; crash handling is native K8s; node-level GPU health (Xid/DCGM) is the health controller's job and invisible to app teams. Namespaces are team/env boundaries ("workspaces" in SDK terms); `BufferPolicy` scopes per-namespace so prod and staging don't contend for warm capacity. Endpoints are plain CRs, so GitOps (ArgoCD/Flux) works with zero adapters — the inference fleet is a reviewed directory of YAML.

## 14. Plugin interfaces

OpenCoda has three pluggable boundaries — `CapacityProvider`, `KVProvider`, and `Engine` — following the same pattern: a small Go interface, in-tree reference implementations, capability/conformance gating for out-of-tree plugins where applicable. Everything else is composition of existing OSS.

### 14.1 CapacityProvider — the capacity boundary

The portability boundary for compute. In-tree providers compile in; out-of-tree load over gRPC (Phase 4). Deliberately small — four methods:

```go
// pkg/capacity/provider.go
type GPURequest struct {
    PoolName     string
    GPUType      string
    GPUCount     int           // per node
    NodeCount    int
    Constraints  Constraints   // region, zone, capacityType, maxHourlyUSD
}

type Offer struct {
    ID            string
    InstanceType  string
    Zone          string
    HourlyUSD     float64       // provider-quoted; scheduler trusts observed over quoted
    ExpiresAt     time.Time
    Interruptible bool          // spot
}

type NodeHandle struct {
    ProviderID   string         // e.g. aws:///us-east-1a/i-0abc...
    NodeName     string         // expected K8s node name on join
    Labels       map[string]string
    LaunchedAt   time.Time
}

type CapacityReport struct {
    Available        int
    RecentICE        []time.Time   // InsufficientCapacity observations
    ObservedHourlyUSD float64
}

type CapacityProvider interface {
    Name() string
    // Quote returns currently-claimed availability and price. Cheap; called often.
    Quote(ctx context.Context, req GPURequest) ([]Offer, error)
    // Provision allocates and bootstraps a node that will join the cluster
    // (cloud-init/userdata installs containerd config, nydus, node agent,
    // kubelet join). Must be idempotent on Offer.ID.
    Provision(ctx context.Context, offer Offer) (*NodeHandle, error)
    Release(ctx context.Context, h *NodeHandle) error
    // Capacity reports OBSERVED reality (ICEs, real prices) for the feedback loop.
    Capacity(ctx context.Context, pool string) (CapacityReport, error)
}
```

**Design notes.**
- *Observed vs. advertised:* providers routinely advertise capacity they can't deliver. Every `Provision` failure with an ICE-class error is recorded into `GPUPool.status.observedCapacity`; the scheduler discounts pools with recent ICEs (exponential decay, half-life 15m). This is the feedback loop that makes greedy fill behave sanely.
- *No SkyPilot dependency.* Same clouds, different consumer: SkyPilot provisions for laptop-launched jobs; OpenCoda's loop owns long-lived buffered nodes inside a controller. We vendor the `skypilot-catalog` price CSVs via a periodic sync job for cross-cloud price awareness — data, not dependency.
- *Conformance suite:* a provider passes if it satisfies idempotency, ICE reporting, join-within-deadline, and release-leak tests against a mock cloud. Gate for out-of-tree listing.

### 14.2 KVProvider — the KV orchestration boundary

LMCache is the default and flagship, but the boundary is formalized — for OSS credibility (a vendor-sponsored project that only works with the vendor's component reads as a trojan horse and suppresses the top-of-funnel §9 depends on) and because the interface makes the moat *legible*: any KV backend plugs in; only LMCache lights up warm restore and affinity routing. The moat is being the best implementation of the boundary, not the only name the code knows.

**Scope discipline:** the engine-side abstraction already exists (vLLM's KV connector API) — we do not reinvent it. `KVProvider` is the *control-plane* boundary: the four places OpenCoda's lifecycle actually touches KV.

```go
// pkg/kv/provider.go
type KVCapabilities struct {
    WarmRestore     bool // post-restore reattach + prefetch (§17.4)
    AffinityHints   bool // prefix fingerprints for gateway routing (§18)
    SharedRemoteTier bool // cross-replica L2
    TierSpill       bool // L1→NVMe→remote cascade
}

type KVProvider interface {
    Name() string
    Capabilities() KVCapabilities
    // Render engine/pod config (env, flags, mounts, sidecars) from the
    // CodaEndpoint kv block + node profile (pinned-mem size, NVMe cache dir).
    RenderPodSpec(ep *CodaEndpoint, node NodeProfile) (PodPatch, error)
    // Node-agent hook: post-restore, pre-readiness. Warm attach + top-k
    // prefix prefetch. No-op if !Capabilities().WarmRestore.
    OnRestore(ctx context.Context, replica ReplicaRef) error
    // Prefix fingerprint for KV-affinity routing; ok=false → router skips affinity.
    Fingerprint(tokens []int) (fp uint64, ok bool)
    Metrics() []prometheus.Collector // hit rate, bytes saved → Studio Tier 3
}
```

**Implementations:** `lmcache` (reference, default in Helm, all capabilities, the only one exercised by Phase 1–3 exit criteria); `null` (testing + "serverless GPUs, no KV magic"); community backends (Redis-class, Mooncake, Dynamo KVBM) plug in with whatever capability subset they support — degradation is graceful and self-advertising via the flags.

**Guardrail:** the interface chases LMCache's capabilities, never the reverse. New LMCache features that don't fit grow a new capability flag; we do not lowest-common-denominator the flagship for hypothetical backends.

### 14.3 `Engine` — the inference-engine boundary

Third pluggable boundary alongside `CapacityProvider` and `KVProvider`. Engines render the inference container; KV layers sidecars/env on top.

```go
type Engine interface {
    Name() string
    RenderPodSpec(ep *CodaEndpoint, node NodeProfile, kvPatch PodPatch) (*PodSpec, error)
    ReadinessProbe() *Probe
    MetricsEndpoint() string   // engine-native scrape path (Phase 2 autoscaling)
    ServedModelID(ep *CodaEndpoint) string
}
```

**v1 implementations:** `vllm` only (`pkg/engine/vllm`) — `vllm serve` args, tensor-parallel from `resources.gpu`, LMCache `kv-transfer-config` when enabled. Admission webhook and validation reject non-`vllm` types.

**Deferred:** `sglang` engine impl + `sglang-router` (Phase 3 router work, FR-14b). Custom `podTemplate` engines remain an extension point, not exercised in v1.

## 15. Buffer controller & scheduler

### 15.1 Reconcile loop

Single controller, one reconcile per `BufferPolicy`, resync 15s + event-driven on demand-signal changes:

```
desiredGPUs = Σ activeReplicaGPUs(endpoints) + bufferTarget(policy)
currentGPUs = Σ ready + provisioning (per pool, per policy scope)

if desired > current:
    plan = Scheduler.Fill(desired - current, eligiblePools)
    for each (pool, n) in plan:
        offers = provider.Quote(...)
        provision up to n, respecting pool.limits & spend ceiling
elif desired < current - hysteresis:
    victims = pick emptiest buffered nodes past stabilizationWindow
    cordon → drain (respect drainTimeout) → provider.Release
```

Invariants: never release a node serving traffic; never exceed `maxHourlyUSD` (checked pre-Provision against observed price); provisioning counts toward `current` to prevent thundering-herd double-provision.

### 15.2 Scheduler interface — greedy now, LP later

```go
type Scheduler interface {
    // Fill decides how to source `need` GPUs across pools.
    Fill(need int, pools []PoolView) (Plan, error)
}
```

**v1 `GreedyScheduler`:** sort pools by `(priority, observedHourlyUSD × ICEPenalty)`, fill in order up to per-pool limits. ~50 lines, debuggable, sufficient until a user has >5 pools.

**Phase 4 `LPScheduler`:** OR-Tools (GLOP) min-cost: minimize Σ costᵢ·xᵢ subject to Σ gpusᵢ·xᵢ ≥ need, xᵢ ≤ limitᵢ, ICE-discounted availability. Identical interface; swap via config. This is the same formulation Modal published — it's a small LP, not a research problem; it's deferred because greedy is *correct enough* and the feedback-loop plumbing matters more.

### 15.3 Health controller

- **Boot:** short active DCGM check (`dcgmi diag -r 1` class) before a node is marked buffer-eligible.
- **Runtime:** watch kernel/Xid events via DCGM exporter; critical Xids → cordon, drain, `Release`, increment pool health metrics.
- **Deep checks:** weekly `dcgmi diag -r 3` on buffered (idle) nodes only — free, since they're idle anyway.

## 16. Image delivery — compose, don't build

**Decision: Nydus over SOCI over custom FUSE.** Rationale: Nydus gives chunk-level content addressing + native zstd + a P2P story (Dragonfly compatibility); SOCI is index-over-existing-OCI (simpler, AWS-centric, weaker dedup); custom FUSE is what Modal built because they predate the ecosystem — for us it's negative-value scope. Trade-off accepted: Nydus requires image conversion; mitigated by `coda image convert` in CI and a conversion webhook for lazy adopters.

**Tiered cache mapping (Modal's table, OSS parts):**

| Tier | Component | Notes |
|---|---|---|
| Page cache | Linux | tune `read_ahead_kb` 128 → 32768 for large sequential image reads; cap to avoid thrash |
| Local NVMe | nydus-snapshotter blob cache | node agent's fill daemon pre-warms per-image **prefetch-manifest** chunk sets for endpoints pinned to the pool |
| In-cluster peer | Spegel (v1) → Dragonfly (scale-out) | Spegel: zero-config P2P registry mirror; Dragonfly when >100 nodes |
| Origin | OCI registry / S3 | zstd layers; **never gzip** (DEFLATE single-threaded ≈100MB/s ceiling) |

The same content-addressed path delivers **snapshot artifacts** and **model weights** (below) — one cache subsystem, three artifact types. Infrastructure compounds.

**Why this is the dominant Phase 1 speedup for engine images.** vLLM/SGLang images are the pathological case: 10–20 GB (CUDA toolkit, PyTorch, cuDNN, NCCL, Triton, flash-attn), where naïve pull stacks three costs — full transfer, single-threaded gzip inflate (~100 MB/s), sequential layer application. The empirical out (Slacker, FAST '16): containers read ~6% of image bytes to start, and engine boot is no exception — interpreter, a subset of torch/CUDA shared objects, engine code; not the other architectures' fatbins or the wheels' test suites. Lazy faulting makes "pull time" stop blocking start (metadata only, sub-second); content addressing means every vLLM endpoint fleet-wide shares 90%+ chunks (identical CUDA/torch), so the first pull anywhere warms every tier for everyone, and an `engine.version` bump transfers only the delta chunks — multi-GB CUDA/torch chunks hash identically and are already resident.

**Prefetch manifests — the engine-matrix superpower.** Because the engine matrix is curated (unlike Modal's arbitrary-user-image problem), each published image carries a chunk-access trace recorded at publish time: exactly which chunks `vllm 0.11` startup touches. The snapshotter prefetches that hot set in the background at container start, and the fill daemon pre-stages it on buffered nodes for pinned endpoints — so on a warmed buffer node the image segment of cold start is effectively zero, not merely fast. Net: 3–8 min pull-and-unpack → ~1s to container start cold, ~0 on warm buffer.

**Segment honesty:** image caching kills exactly one of the four cold-start segments — "load filesystem state." `import torch`/engine init is the CPU snapshot's kill (Phase 2), CUDA graphs/device setup the GPU snapshot's (Phase 3), weights a separate throughput-bound path (§17.5). In Phase 1, image caching + buffer *is* the speedup story — hence the honest ≤60s p50 target there, with ≤20s waiting on Phase 3.

**Registry requirements: none beyond OCI.** Nydus-converted images push to any OCI-distribution-compliant registry (Harbor, ghcr, ECR, GAR, Docker Hub); a cloud's artifact registry is a locality/egress optimization, never a dependency. Nydus also supports object storage directly as the blob backend: manifest/metadata in any registry, content-addressed chunks in S3/GCS/**Garage** — the same bucket family holding snapshots, weights, and the LMCache cold tier. Minimum-moving-parts v1 deployment: in-cluster **Garage** (S3-compatible) + any registry; MinIO and cloud object stores remain compatible remote tiers.

## 17. Node agent & snapshot subsystem

### 17.1 Runtime handler

DaemonSet `coda-node-agent` registers a containerd runtime handler `coda`. Pod creation for snapshot-classed endpoints flows: gateway scale-up → pod scheduled to buffered node → handler consults snapshot cache:

```
key = H(imageDigest ‖ modelRef ‖ engineVersion ‖ cpuFeatureSet
        ‖ kernelABI ‖ nvidiaDriverVersion ‖ gpuType ‖ snapshotClassGen)

if cache[key] exists and validates → RESTORE path
else → COLD BOOT path; if SnapshotClass present, checkpoint at readiness,
      upload keyed artifact (async, off hot path)
```

**Key lesson encoded:** CPU feature flags are part of the key. A snapshot baked on a host with `pclmulqdq` (or any ISA extension) SIGILLs on a host without it. Restore requires the restore-host feature set ⊇ checkpoint-host set; the scheduler prefers exact-match pools. On any key miss or restore failure: **fall back to cold boot, never fail the request path.** Restore is an optimization, not a dependency.

### 17.2 CPU checkpoint/restore (Phase 2)

**Decision: CRIU on runc, not gVisor.** Modal runs gVisor because they're multi-tenant-hostile-code paranoid and runsc's userspace kernel makes C/R structurally easy. OpenCoda users run their own code on their own clusters; the gVisor syscall tax and operational complexity buy less. CRIU + runc keeps the stock container stack. Trade-off accepted: CRIU is sensitive to kernel versions and exotic fds — constrained by (a) controlling the engine images we snapshot, (b) the compatibility key, (c) cold-boot fallback. Revisit if a managed multi-tenant TensorCoda tier ever needs sandboxing.

Mechanics: checkpoint fires at engine readiness (after imports, tokenizer load, torch.compile, CUDA graph capture — the tens-of-seconds CPU-side grind). Artifacts stored uncompressed (`pages.img` is 100MB–multi-GB; restore is won on how fast it hits page cache; gzip would bottleneck it). Delivered via §16's cache; fill daemon pre-warms NVMe for pinned endpoints.

### 17.3 GPU checkpoint/restore (Phase 3)

NVIDIA `cuda-checkpoint` (driver r550+) via its CRIU plugin — the device path rides the CPU path: driver migrates device memory → host memory pre-checkpoint, restores it post-restore. One agent, one more plugin.

Engine prep (per `SnapshotClass.pre`):
1. **Weight offload to host** before checkpoint (vLLM/SGLang sleep-mode / level-2 offload) — keeps the device image small and restore-friendly.
2. **Drop the KV cache region** from the snapshot. Empty KV is faster to recreate than restore — Modal's finding, and ours, because:

### 17.4 Warm restore — the LMCache wedge

Where OpenCoda stops being a Modal clone. On restore, the engine recreates an *empty* device KV region, then the LMCache connector attaches to the endpoint's **shared persistent KV pool** (CPU RAM of peers → local NVMe → object storage). First requests hit cached prefixes (system prompts, tool schemas, repo context, conversation history) instead of re-prefilling them.

Sequence (restore path, target ≤20s p50):
```
t0    pod scheduled to buffered node (buffer hit: no instance alloc)
t0+1s snapshot key match; pages.img streaming from NVMe/peer
t0+8s CRIU restore complete; cuda-checkpoint restores device memory
t0+10s engine process live; KV region allocated empty
t0+11s LMCache connector attaches to shared pool; hot prefixes
       prefetched for endpoint's top-k prefix fingerprints
t0+12s readiness; first request: ~90% prefix hit on agent traffic
```

KV-affinity routing (gateway): requests carry a prefix fingerprint via `KVProvider.Fingerprint` (LMCache's chunk-hash chain, ~256-token chunks); router prefers replicas whose local tiers already hold those chunks, falling back to the shared remote tier. This converts cross-replica sharing from "lucky" to "engineered." Routing degrades to plain least-loaded when the provider lacks `AffinityHints`. **Implementation (§18):** rather than hand-build this dispatch, Phase 3 adopts the engine-native router (`vllm-router`/`sglang-router`), whose KV-aware mode already consumes LMCache's KV-event stream via the centralized LMCache controller — the `KVProvider.AffinityHints` capability is satisfied by wiring that stream, not by reimplementing it. The thin Go proxy remains the fallback when the engine has no native router or the provider lacks `AffinityHints`.

**Tier ownership, for precision:** L0 (GPU HBM) is owned by the engine's paged allocator — LMCache interfaces with it via the engine's KV connector API, never owns it. LMCache manages movement: L0→L1 offload into pinned CPU memory over dedicated CUDA streams overlapped with compute (decode never stalls); L1→L2 spill (LRU) to local NVMe, then the remote backend named by `remoteRef`. Chunk granularity means a long context can be partially hot (L1) and partially cold (S3) and still assemble into one hit. OpenCoda's additions are cluster-shaping: the node agent sizes pinned memory and NVMe cache dirs from the node profile, and all replicas of an endpoint share one L2 namespace — the attach point for both cross-replica sharing and warm restore. Fetch-vs-recompute breakeven (pull KV from L2 only when fetch bandwidth beats prefill FLOPs) is decided per-chunk by LMCache; OpenCoda exposes the knobs in the CRD.

### 17.5 Weight loading

Throughput-bound, not snapshot-skippable (a few GB/s from object storage; seconds → hundreds of seconds depending on model size). v1: weights as content-addressed chunks through §16's cache (NVMe-resident for pinned endpoints ⇒ near-instant on warm nodes). Extension point, not built:

```go
type WeightSource interface {
    Open(ctx context.Context, ref ModelRef) (io.ReaderAt, int64, error)
}
```

Future implementations (community/commercial): in-AZ weight server, RDMA/RoCE. Explicitly out of scope — do not get nerd-sniped.

### 17.6 Multi-GPU endpoints — serving now, snapshots later

Multi-GPU *serving* (TP/PP) is a v1 capability with no special handling: a TP=8 endpoint is a pod requesting 8 GPUs, and the buffer provisions whole 8-GPU nodes anyway. Buffer hits, lazy pulls, autoscaling, health checks, and **LMCache warm attach** all apply — KV persistence is orthogonal to tensor parallelism (TP shards KV across heads; LMCache handles TP-sharded chunk layouts). A cold TP=8 replica still comes up with warm prefixes, which the disposable-KV platforms cannot offer at any GPU count.

Multi-GPU *checkpoint/restore* is deferred (see Non-goals): NCCL communicators do not survive a pause. The eventual path (Phase 4+, flag-gated) is **coordinated checkpoint** — barrier all ranks, drain in-flight collectives, tear down NCCL communicators cleanly, checkpoint each rank via cuda-checkpoint, restore all ranks, re-init NCCL (seconds, not the minutes of full boot). Requires engine hooks adjacent to vLLM sleep mode plus an agent-side rank coordinator. The snapshot key already carries `gpuType`; coordinated C/R adds topology/interconnect to the key. NVIDIA's expanding multi-device cuda-checkpoint support may cheapen this over time — one more reason shipping it last is correct rather than cowardly. Deferral cost is low regardless: large-model cold start is weight-load-bound (§17.5), where snapshots help least and `WeightSource` helps most.

### 17.7 Runtime classes & snapshot gating

Sandboxed runtimes are consumed via Kubernetes-native `RuntimeClass` — a name→containerd-runtime-handler mapping that already exists in the stack; OpenCoda builds no abstraction here, only three integration points: (1) `CodaEndpoint.spec.runtime.class` stamps `runtimeClassName` onto pods; (2) the admission webhook rejects `SnapshotClass` on non-runc runtimes (the `coda` snapshot handler wraps runc; sandboxed pods take the cold-boot path, enforced at admission, not discovered at restore time); (3) node bootstrap installs configured runtimes per `GPUPool` and the node agent labels nodes with supported runtimes for scheduling.

GPU-shaped caveats per runtime: **runc** — default, full snapshot path. **gVisor** — opt-in for untrusted function workloads (§19.2); GPU access via `nvproxy`; cold-boot only (gVisor has its own C/R mechanism; maintaining two checkpoint stacks is rejected scope). **Kata** — microVM with VFIO GPU passthrough: dedicated-device semantics, guest driver/toolkit complexity, and boot latency that fights the cold-start thesis; documented as possible, recommended against, revisit on design-partner demand.

## 18. Gateway: routing & autoscaling

**Control plane vs. data plane split.** OpenCoda owns the control plane — *how many replicas exist and when they wake* (scale-from-zero, buffer-backed wakeups, queueing during 0→1, demand export). Request-level dispatch among *live* replicas — *which warm worker gets this request* — is a data-plane concern that mature engine-specific routers already solve, so v1 composes rather than builds (§16 ethos). The two are layered, not merged: the autoscaler/buffer logic below sits above whatever router dispatches, and feeds it the live replica set via Kubernetes service discovery.

- **Router (pluggable, engine-specific):** a `Router` boundary in front of each endpoint, with per-engine implementations selected by `engine.type`:
  - **Phase 1 — thin Go proxy (default/fallback):** least-loaded + session-affinity dispatch, queueing during 0→1, per-endpoint concurrency accounting. Correct and engine-agnostic; the `null`-equivalent path that always works.
  - **Phase 3 — `vllm-router` / `sglang-router` (KV-aware):** adopt the engine-native router when KV-affinity routing lands (§17.4). [`vllm-project/router`](https://github.com/vllm-project/router) already implements cache-aware / consistent-hash / power-of-two dispatch and **KV-aware routing off LMCache's native KV-event stream** (via a centralized LMCache controller), plus P/D disaggregation (§25, FR-15a) — exactly the surface §17.4 would otherwise hand-build. SGLang endpoints route via `sglang-router`; the engine-specific router is scoped behind the `Router` boundary because it is engine-coupled, mirroring the `KVProvider` pattern (reference impl + capability flags + graceful degradation to the thin proxy).
- **Multi-model front door (additive):** an optional single OpenAI-compatible ingress where the request `model` field dispatches to the owning `CodaEndpoint`'s replica set. This does **not** dissolve the endpoint — `CodaEndpoint` remains the lifecycle unit that KV-pool namespacing, autoscaling, scale-to-zero, snapshot keying, and $ attribution all bind to (model-as-arg is a *serving-surface* convenience, not a data model). Primary value is the UC4 many-model long tail: dozens of low-traffic endpoints behind one URL. Per-endpoint URLs and the aggregated front door coexist.
- **Autoscaler:** per-endpoint loop on (in-flight concurrency, queue depth, queue age). 0→1 wakes via buffer; 1→N adds replicas while queueing absorbs the edge (bounded queue, backpressure via 429 + Retry-After past SLO). N→0 after `scaleToZeroAfter` idle. The router never owns replica count — it dispatches among whatever OpenCoda has made live.
- **Demand signal export:** the autoscaler's forecast (EWMA of arrivals + volatility) feeds `BufferPolicy.dynamic` — the buffer pre-scales on volatility, not just level.

## 19. SDK & Modal compatibility

**Goal:** existing Modal code migrates with an import change and a config file, not a rewrite. The compat target is Modal's five load-bearing primitives: `App`, `@app.function(gpu=..., image=...)`, `modal.Image.*` builders, `@app.cls()` + `@modal.enter(snap=True)`, and `.remote()` / `deploy|run|serve`.

### 19.1 Package layout
- `coda` — the native SDK (idiomatic, CRD-aware).
- `coda.compat` — Modal-shaped shim over the native SDK (`import coda.compat as modal`); emits warnings on unsupported surface rather than failing silently.

### 19.2 Function shipping
`@app.function` work is cloudpickled and submitted over the gateway gRPC API to a generic **runner image** containing a small agent that deserializes and executes (inputs/outputs over the same channel). Mechanically, `coda deploy` materializes a `CodaEndpoint` (serving) or `CodaFunction` (job/batch) CR. `@modal.enter(snap=True)` maps directly onto `SnapshotClass.checkpointAfter` semantics — the compat shim translates it to a snapshot-classed endpoint.

### 19.3 Server-side image builds
`Image` definitions cannot build on the laptop and match Modal UX. An in-cluster **BuildKit** job consumes the SDK's image definition, builds, converts to Nydus, pushes to the configured registry, and returns a digest the endpoint pins. Build logs stream back through `coda deploy` and Studio.

### 19.4 Authentication & connection
Self-hosted equivalent of Modal's token pair:

```
CODA_SERVER_URL=https://coda.internal.yourco.com   # gateway ingress
CODA_TOKEN_ID / CODA_TOKEN_SECRET                  # or ~/.coda/config via `coda token new`
```

Tokens are `CodaToken` CRs validated by the gateway — revocation is `kubectl delete`, audit is the K8s audit log. Bootstrap rides existing kubeconfig RBAC: anyone who can create the CR can mint a token (no signup flow). "Workspace" maps to namespace. OIDC/SSO is a TensorCoda-tier upgrade.

### 19.5 Documented gaps (do not fake)
`Dict`/`Queue` require an optional backing component (NATS or Redis); `Volume` maps to PVC/S3 mounts with differing consistency semantics; Sandboxes out of scope for v1. Each compat-shim gap raises a descriptive error pointing at the native alternative.

## 20. Deployment topology

The control plane is ordinary Kubernetes workloads in `opencoda-system`, installed by the Helm chart (`charts/opencoda/`): controller-manager, gateway, webhook, and Studio as Deployments; node-agent as a DaemonSet on GPU nodes. In-cluster **Garage** provides the default S3-compatible object store for LMCache cold tier, Nydus blobs, weights, and snapshot artifacts (values: `garage.endpoint`). The "API" is two surfaces: the K8s API extended by CRDs (`opencoda.dev/v1alpha1`) and the gateway REST (SDK/CLI/Studio); gRPC SDK surface is stubbed.

**Invariant:** control-plane pods schedule onto **static CPU nodes only** (enforced via Helm-set node affinity) — never onto GPU pools the buffer controller manages, or scale-to-zero eats its own brain. Control-plane outage behavior per §24: data plane keeps serving, scaling pauses.

## 21. OpenCoda Studio

A **Next.js 16+ App Router** app (`studio/`: pnpm, TypeScript, shadcn/ui, Tailwind, `output: 'standalone'`) deployed in `opencoda-system` and fronted by the gateway. Data via gateway REST routes (K8s API for resource state, Loki for logs when wired; Tier 1 includes demo fallbacks for local dev). No separate backend product — Studio is a rendering layer over §22's metrics; it is mostly a frontend problem, not a data problem.

**Memory horizon (the invariant that keeps it a rendering layer):** *live state* comes from the K8s API (free, no retention); *bounded history* — control-plane events, completed-job and replica records, per-call logs — lives in Loki + Prometheus + TTL'd CR/record retention on the order of days-to-weeks, still no new infrastructure; *unbounded/org-scale history* (long-term audit, per-call trace warehousing, quarter-spanning chargeback) is the ClickHouse "if/when needed" escape hatch and the TensorCoda line (§9). Studio remembers weeks; TensorCoda remembers everything.

Modal's dashboard earns its reputation by making the invisible lifecycle visible. Priority order:

| Tier | Ships | Contents |
|---|---|---|
| **1 — Table stakes** | Phase 1 | Live endpoint/function list with replica states; **per-call streaming logs** (80% of why anyone opens a dashboard) |
| **2 — Signature view** | Phase 2 | **Replica lifecycle timeline**: alloc → image → host-restore → device-restore → KV-attach → serving, per-segment timings. Best debugging tool + a live demo of the architecture; every cold start renders the pitch |
| **2a — Fleet view** | Phase 2 | **GPU fleet/node inventory**: per-pool node list with lifecycle state (provisioning → boot-check → buffered → active → cordoned/draining), per-GPU DCGM telemetry (utilization, memory, temperature, power, Xid event history), boot/deep health-check results, and which endpoint replicas each GPU currently serves. Drill-down: pool → node → GPU → replica, linking into the Tier 2 timeline. Pure rendering over the DCGM exporter and `GPUPool`/node status — no new collectors. The operator's answer to "what is my hardware doing and why was this node drained," without leaving Studio for kubectl/Grafana |
| **2b — History & events** | Phase 2 | **Control-plane event feed + bounded history**: a filterable, retained log of consequential decisions (provision/release, ICE-driven pool spill, cordon/drain on Xid, snapshot bake/invalidate, restore→cold-boot fallback, scale-to-zero), filterable per pool / node / endpoint / job; plus **completed-job and replica history** — which engine/version/model served which endpoint on which GPU, when, via which cold-start path, with Loki-backed logs for finished runs. Controllers emit K8s Events (standard hygiene) and mirror them as structured Loki lines, since native Events expire (~1h TTL) and can't be the record. Completes the debugging loop: Tier 2 timeline (what happened to a replica) + this feed (what the control plane decided and why). The answer to "why did my node disappear at 2am" and "show me last Tuesday's job output" |
| **3 — The one Modal can't copy** | Phase 3 | **Economics dashboard**: live demand-vs-provisioned curve, allocation utilization, per-pool observed prices & ICE history, LMCache hit rate & prefill GPU-seconds avoided, running "$ saved vs. static provisioning" counter. Modal hides cost behind billing because their incentive is your spend; OpenCoda's incentive is your savings, so savings are the home screen. Doubles as TensorCoda sales material |

## 22. Observability

Prometheus metrics + OTel traces. The headline set (also the TensorCoda dashboard's raw material):

- `coda_allocation_utilization` = GPU-seconds running app code ÷ GPU-seconds provisioned (the North Star).
- `coda_cold_start_seconds{path=cold|cpu_restore|gpu_restore}` histogram, per phase breakdown (alloc/image/host-init/device-init).
- `coda_kv_hit_rate{endpoint}`, `coda_kv_bytes_saved_total` (prefill compute avoided).
- `coda_pool_observed_hourly_usd`, `coda_pool_ice_total`, `coda_buffer_size`.
- Node/GPU telemetry for the Fleet view: DCGM exporter series (utilization, memory, temperature, power, Xid counts) joined with `coda_node_lifecycle_state{pool,node}` and `coda_node_health_check_result{check=boot|deep}` — emitted by the health controller, not a new collector.
- Per-endpoint $ estimate = Σ node-hours × observed price, attributable for chargeback.
- Control-plane events for the History & events view: controllers emit K8s Events and mirror them as structured Loki lines (`reason`, `pool`/`node`/`endpoint`/`job` labels, decision context) — native Events expire (~1h) and are insufficient as a record; completed-job and replica records are TTL'd CR status snapshots, not a new store.

## 23. Security & tenancy

- v1 trust model: single-org cluster; namespace isolation between teams; standard RBAC on CRDs.
- Node-agent runs privileged (CRIU requires CAP_SYS_ADMIN-class caps); scoped via seccomp/AppArmor profiles; snapshot artifacts encrypted at rest (SSE) — they contain process memory, treat as secrets-equivalent.
- Provider credentials: per-pool Secrets, least-privilege IAM (instance lifecycle only); spend ceilings enforced control-plane-side as defense in depth.
- Trust model is *cooperative* multi-tenancy (one org's engineers), not Modal-style adversarial multi-tenancy — standard K8s hygiene is the proportionate control, matching the posture of every other internal platform the org runs. The arbitrary-code surface arrives with function shipping (§19.2): hardened runner pods (non-privileged, seccomp, no host mounts, egress-restricted NetworkPolicy) by default, with gVisor opt-in via `runtime.class` (§17.7) for orgs that want sandboxing.
- Hostile-adversarial sandboxing as a *default* (gVisor/Kata mandatory): deferred to a hosted multi-tenant TensorCoda tier, where it stops being optional.

## 24. Failure modes & degradation ladder

| Failure | Behavior |
|---|---|
| Snapshot restore fails / key miss | Cold boot. Log, increment metric, never fail request. |
| Provider ICE storm | ICE penalty discounts pool; scheduler spills to next pool; alert if all pools exhausted. |
| Buffer empty during spike | Queue at gateway up to SLO bound; provision hot-path; shed with 429 past bound. |
| Node Xid critical | Cordon → drain → release → backfill from buffer. |
| LMCache remote tier down | Engines degrade to local tiers → empty cache; correctness unaffected (cache is acceleration, never truth). |
| KVProvider lacks capability (e.g. `AffinityHints`) | Feature self-disables: router falls to least-loaded; restore skips warm attach. Advertised via capability flags, never a runtime surprise. |
| `SnapshotClass` on sandboxed runtime | Rejected at admission (§17.7) — config error surfaces at `kubectl apply`, not at 2am restore failure. |
| Control plane down | Data plane keeps serving (gateway + pods are independent); no scaling until recovery. Standard operator posture. |

## 25. Steady-state serving & the P/D roadmap

A deliberate non-resemblance to Modal: their DNA is function-shaped work, where the edges (spin-up/spin-down) are the whole game. OpenCoda's core primitive is the long-running `CodaEndpoint` — functions are the compat afterthought (§19), the inverse of Modal's hierarchy. Consequences:

**Steady state is untouched engine performance.** OpenCoda is not in the per-token data path — no wrapper runtime, no platform tax. Continuous batching, paged attention, and goodput-per-GPU belong to the engine, unmodified. The serverless machinery fires only at the edges; an endpoint with `minReplicas: 4` and no scale-to-zero never invokes it — yet the buffer still earns keep at steady state as spike absorber, spot-interruption backfill, and Xid-failure replacement pool. The graceful degradation target: with serverless features off, OpenCoda is simply a good Kubernetes serving operator with self-healing capacity.

**The KV layer is a steady-state feature wearing serverless clothing.** Shared L2 + affinity routing pay per-request on fleets that never scale to zero — cross-replica prefix sharing and prefill avoidance are continuous-traffic wins independent of how a replica was born.

**Engine-grade signals (FR-12d).** Concurrency/queue-depth scaling is what you do when you can't see inside the workload; we can. Engines export KV-cache utilization, pending prefill tokens, TTFT/TPOT — scaling on "KV 85% full, TTFT drifting toward SLO" beats raw concurrency. The gateway accepts per-endpoint latency SLOs driving both scaling and shedding; mostly a policy layer over existing machinery.

**Prefill/decode disaggregation (FR-15a, Phase 4+ flag-gated).** The serving frontier splits prefill and decode onto separate pools (different batching dynamics, different hardware sweet spots), requiring fast prefill→decode KV transfer — which is *literally LMCache's other flagship job*. OpenCoda is uniquely positioned to orchestrate it: heterogeneous replica roles within one endpoint (`prefill` pool on one GPU class, `decode` on another, potentially different `GPUPool`s), role-aware two-hop gateway routing, LMCache as transport — with buffer/snapshot economics applied per role. Dynamo ships this as a monolithic stack; composable CRDs on customer-owned multi-cloud capacity is an assembly nobody else can make from these parts. It belongs in the doc now so the endpoint spec, gateway, and `KVProvider` keep the door open: the role concept must not be precluded by v1 API choices (replica role is a label + routing concern, not a new CRD).

One-line positioning, for the pitch and the AIBrix/Dynamo comparison row: **serverless when you're bursty, best-in-class serving operator when you're not.**

## 26. Phased rollout

| Phase | Scope | Exit criteria |
|---|---|---|
| **1 — Wedge** (8–10 wk) | CRDs, buffer controller + greedy, AWS/GCP/Static providers, Nydus, LMCache default wiring, gateway, health ctrl, SDK/CLI + token auth, engine matrix + prefetch manifests, Studio Tier 1 | **Exit met July 2026** on `opencoda-dev`: cold-start p50 **39.1s** (≤60s); KV hit **94%** (≥70%); gateway curl + 429→200 scale-from-zero **40s** wall time |
| **1a — Scaffold shipped** (June 2026) | Monorepo scaffold + Phase 1 depth pass (§29) | Build passes; `make e2e-kind` harness; `hack/e2e-aws.sh` for spot run |
| **1b — EKS static GPU gate** (July 2026) | Live EKS validation: static `GPUPool` + buffer + `CodaEndpoint` → fakevllm Ready on g5 (`make e2e-eks-gpu`) | Gate passed on `opencoda-dev` (us-east-1); prerequisite to **start Phase 2** implementation cleared — Phase 1 §26 wedge metrics automation shipped July 2026; live measured sign-off pending fresh `aws login` run |
| **2 — CPU snapshots** (6–8 wk) | Node agent runtime handler, CRIU path, snapshot keying + cache, in-cluster BuildKit builder, Modal compat shim, engine-metric/SLO autoscaling, Studio Tier 2 | `import torch`-class init skipped; host-init segment ≥5x faster; restore-failure fallback proven in chaos test |
| **3 — GPU snapshots** (8–12 wk) | cuda-checkpoint, weight offload, warm restore + KV-affinity routing via engine-native router (`vllm-router`/`sglang-router`, FR-14b), Studio Tier 3 (economics) | ≤20s p50 cold start; ≥85% first-request KV hit; 10k-cold-start soak with zero request-path failures |
| **4 — Scale-out** (ongoing) | LP scheduler, provider conformance + out-of-tree plugins, P/D disaggregation experiment, federation API stubs | 2 external provider plugins; design partner running >100 GPUs across ≥2 clouds |

Risk-ordering rationale: the jankiest component (GPU C/R) ships **after** users exist; the API surfaces people build against (CRDs, CapacityProvider, LMCache wiring) ship first and must be right on day one.

## 27. Open questions

1. Snapshot artifact format versioning across CRIU releases — pin CRIU per node-agent release, or negotiate?
2. ~~Prefix-fingerprint scheme for KV-affinity routing: first-N-token hash vs. LMCache-native chunk hashes.~~ **Resolved (§18, FR-14b):** adopt the engine-native router (`vllm-router`/`sglang-router`), whose KV-aware mode consumes LMCache's native KV-event stream — native chunk hashes, not a first-N-token approximation. Remaining sub-question: pin the LMCache-controller protocol version per gateway release.
3. Spot interruption handling: 2-min warnings → proactive checkpoint-and-migrate, or drain-only in v1? (Lean drain-only; migration is Phase 4+.)
4. Dynamic buffer formula: stddev-based vs. quantile-of-arrivals — needs the reference trace benchmark to decide empirically.
5. Conversion-free Nydus adoption (runtime conversion proxy) — worth the complexity vs. mandating CI conversion?
6. Coordinated multi-GPU checkpoint (§17.6): engine-hook design — extend vLLM sleep mode upstream, or carry a patch until upstreamed?
7. ~~`KVProvider.Fingerprint` granularity: per-request first-N-token hash vs. exposing LMCache's full chunk-hash chain to the router.~~ **Resolved (§18, FR-14b):** the engine-native router consumes LMCache's full chunk-hash chain directly via the KV-event stream; `KVProvider.Fingerprint` is needed only on the thin-proxy fallback path, where first-N-token hashing is sufficient.
8. P/D role surface (§25): role-as-label on one endpoint vs. paired endpoints with a binding — which keeps the v1 API forward-compatible with less ceremony?

## 29. Phase 1 implementation status (June–July 2026)

Initial monorepo scaffold landed at `github.com/immanuel-peter/opencoda` (CRD group **`opencoda.dev`**, not `opencoda.io`). **July 2026 live sign-off on `opencoda-dev` (us-east-1):** gateway traffic curl + scale-from-zero passed; `status.coldStart.p50ms=39122` (39.1s); `status.kvHitRate=0.94`; harness cold-start wall time 40s (429→200). Phase 2 implementation may proceed.

### Shipped (Phase 1 depth pass — June 2026)

| Area | Location | Notes |
|---|---|---|
| CRDs + admission | `api/v1alpha1/`, `config/crd/bases/`, `internal/webhook/` | All six resources; `GPUPool.status.nodeRecords`; rejects non-`runc` + `SnapshotClass`, non-`vllm` engine |
| CapacityProvider factory | `pkg/capacity/`, `internal/capacityfactory/`, `pkg/capacity/{static,aws,gcp}/` | Per-pool factories from `credentialsRef`; bootstrap userdata (`pkg/capacity/bootstrap/`); AWS EC2 spot + ICE; GCP GCE SPOT + exhaustion |
| Pool controller + pricesync | `internal/controller/pool/`, `pkg/capacity/pricesync/` | `observedCapacity`, node record reconciliation, skypilot-catalog CSV sync; **static pool:** discovers pre-joined nodes via `opencoda.dev/pool` label (skips `Provision()` when nodes exist) |
| Buffer depth | `internal/controller/buffer/` | Real warm GPU counts, scale-down (cordon/drain/Release), `maxHourlyUSD` ceiling, dynamic target from gateway demand EWMA; skips static `Provision()` when pool has real nodes |
| Endpoint depth | `internal/controller/endpoint/` | `opencoda.dev/desired-replicas`, scale-to-zero timer, spec-hash rolling upgrades + rollout condition; GPU pods: `nvidia.com/gpu` requests, `opencoda.io/gpu` tolerations, `opencoda.dev/gpu` nodeSelector; rollout condition `lastTransitionTime` fix |
| Health + Xid | `internal/controller/health/` | Boot/deep check; `opencoda.dev/xid-critical` → cordon/drain/Release |
| Gateway ↔ K8s | `internal/gateway/`, `cmd/coda-gateway/` | K8s client, pod URL registration, autoscaler patches desired-replicas, EWMA → `BufferPolicy.status.demandEWMA` |
| CodaToken auth | `internal/gateway/token.go`, `cli/token.go` | CR lookup + sha256 verify; `coda token new` |
| Cachefill + Spegel | `internal/nodeagent/cachefill/`, `charts/opencoda/` | ctr pull + nydus prefetch; Spegel DaemonSet + containerd config in userdata |
| E2E harness | `test/e2e/`, `hack/e2e-kind.sh`, `hack/e2e-aws.sh`, `hack/e2e-eks.sh`, `hack/e2e-eks-gpu.sh`, `hack/e2e-eks-vllm.sh`, `hack/e2e-uc1.sh`, `hack/e2e-nydus.sh`, `hack/e2e-phase1-signoff.sh` | fakevllm image; kind + `make e2e-kind`; EKS control-plane smoke; **`make e2e-eks-gpu`** static g5 gate + automated gateway curl/429→200; **`make e2e-eks-vllm`** real Qwen2.5-0.5B wedge; **`make e2e-uc1`** bursty agent trace |
| EKS GPU gate (July 2026) | `hack/e2e-eks-gpu.sh`, `config/eks/gpu-nodegroup.yaml`, `hack/lib/e2e-gateway.sh`, GHCR images | g5.xlarge static nodegroup + NVIDIA device plugin; fakevllm Ready on labeled GPU node; CodaToken + gateway `/v1/chat/completions` + scale-from-zero loop |
| Cold-start + KV metrics (July 2026) | `internal/controller/endpoint/`, `internal/gateway/k8s.go`, `internal/metrics/` | `coda_cold_start_seconds` observed on pod Ready; `status.coldStart.{p50ms,p95ms}`; gateway scrapes prefix-cache counters → `coda_kv_hit_rate` + `status.kvHitRate` |
| LMCache Garage remote tier (July 2026) | `pkg/kv/lmcache/lmcache.go`, `hack/lib/garage-bootstrap.sh`, `charts/opencoda/` | Pinned `lmcache/lmcache:v0.3.2`; `--l2-adapter` S3 JSON for Garage; `garage-s3-credentials` secret; Garage ConfigMap + data volume in Helm |
| UC1 loadgen (July 2026) | `test/e2e/loadgen/`, `hack/e2e-uc1.sh` | Bursty multi-turn agent trace; reports utilization + first-request KV hit |
| DCGM exporter (July 2026) | `charts/opencoda/` (`dcgmExporter.enabled`) | `dcgm-exporter` DaemonSet on `opencoda.dev/gpu` nodes; Xid actuation remains `opencoda.dev/xid-critical` annotation path |
| Nydus + cachefill e2e (July 2026) | `hack/Dockerfile.node-agent`, `config/eks/gpu-nodegroup.yaml`, `hack/e2e-nydus.sh` | node-agent image with `ctr` + `nydus-image`; GPU userdata containerd certs + nydus binaries; cachefill DaemonSet with `--images` |
| CI + image publish | `.github/workflows/ci.yml`, `.github/workflows/publish-images.yml` | Unit/vet/kind on GHA; parallel GHCR publish for controller, gateway, studio, fakevllm |
| Metrics | `internal/metrics/` | Prometheus series per §22 |
| Image convert | `cli/image_convert.go` | Wraps `nydusify convert` |
| SDK + CLI | `sdk/python/`, `cmd/coda`, `cli/` | Native client; `coda.compat` stub |
| Studio Tier 1 | `studio/` | Next.js 16 App Router, industrial dark UI |
| Helm | `charts/opencoda/` | Controller, gateway, studio, node-agent, **Garage**, Spegel, credential secret refs |
| Engine abstraction | `pkg/engine/`, `pkg/engine/vllm/` | FR-5b |
| KVProvider | `pkg/kv/lmcache/`, `pkg/kv/null/` | LMCache MP mode; Garage as default remote tier |

### Remaining optional follow-ups (non-blocking for Phase 2)

| Gap | FR | Notes |
|---|---|---|
| **UC1 utilization number** | §26 | KV hit + cold start met on fakevllm; full bursty utilization soak can rerun with `UC1_IDLE_SEC=10 make e2e-uc1` |
| **Live spot node join** | §26 | `hack/e2e-aws.sh` now patches `aws-spot` buffer to `minWarmGPUs: 1`; rerun after buffer reconcile |
| **Real vLLM + Garage tier spill** | §16 | `make e2e-eks-vllm` + `hack/lib/lmcache-tier-spill.sh` (Qwen2.5-0.5B on g5) |
| **Nydus cachefill on EKS** | FR-4 | `make e2e-nydus` after `nydusify` convert + node-agent image on amd64 ECR |

### Live sign-off results (July 2026, `opencoda-dev`)

| Metric | Target | Measured |
|---|---|---|
| Cold-start p50 (`status.coldStart.p50ms`) | ≤60s | **39.1s** |
| KV hit (`status.kvHitRate`) | ≥70% | **94%** |
| Gateway 429→200 wall time | — | **40s** |
| Gateway `/v1/chat/completions` curl | 200 | **pass** |
| Scale-from-zero (429 + Retry-After) | pass | **pass** |

### Previously open — automation now shipped (July 2026)

| Capability | Location | Status |
|---|---|---|
| Gateway traffic on EKS | `hack/lib/e2e-gateway.sh`, `CODA_GATEWAY_TEST=1` in `hack/e2e-eks-gpu.sh` | Automated CodaToken + curl + 429/scale-from-zero |
| Measured cold start + KV hit | `internal/controller/endpoint/`, `internal/gateway/k8s.go`, `test/e2e/loadgen/` | Instrumentation + UC1 harness; live numbers pending run |
| Live EKS spot run | `hack/e2e-aws.sh` (`AWS_SPOT_SUBNETS`) | Script ready; live run pending |
| DCGM Xid in prod | `charts/opencoda/` dcgm-exporter DaemonSet | Exporter manifest shipped; live scrape pending |
| Nydus + cachefill end-to-end | `hack/e2e-nydus.sh`, `hack/Dockerfile.node-agent` | Script + image ready; live run pending |

### Previously scaffold-only — now shipped (removed from gap list)

AWS/GCP real provisioning, pool feedback, buffer scale-down, gateway K8s wiring, CodaToken validation, FR-1a rolling upgrades, cachefill+Spegel, Xid automation, kind E2E harness, **EKS static GPU gate** (pool static-node discovery, endpoint GPU pod spec, `make e2e-eks-gpu`).

### Explicitly deferred to Phase 2 or 3 (do **not** block Phase 1 on these)

| Capability | Phase | FR |
|---|---|---|
| Node agent **runtime handler**, CRIU checkpoint/restore, snapshot manager | Phase 2 | FR-10, FR-11, FR-12 |
| In-cluster BuildKit + **Modal compat shim** | Phase 2 | FR-12a, FR-12b |
| Studio Tier 2 / 2a / 2b (timeline, fleet, history) | Phase 2 | FR-12c–f |
| Engine-grade autoscaling (TTFT/TPOT, latency SLOs) | Phase 2 | FR-12d |
| **cuda-checkpoint**, warm restore, snapshot compatibility solver | Phase 3 | FR-13, FR-14 |
| **KV-affinity routing** via `vllm-router` / `sglang-router` | Phase 3 | FR-14b |
| Studio Tier 3 economics dashboard | Phase 3 | FR-14a |
| **SGLang** `Engine` implementation | Post-v1 (interface ready) | FR-5b extension; router in Phase 3 when added |

**Summary:** Phase 1 §26 wedge exit criteria are **met** on live EKS (July 2026). Node-agent CRIU, snapshots, Modal compat, and KV-aware routing remain **Phase 2–3** by design. **Phase 2 implementation may proceed.**

## 28. What we'd revisit as it grows

Greedy→LP scheduler (planned); Spegel→Dragonfly past ~100 nodes; runc/CRIU→sandboxed runtime if a managed multi-tenant tier emerges; single-GPU snapshot constraint as NCCL pause/resume matures upstream; weight delivery via in-AZ/RDMA `WeightSource` once a design partner is genuinely bottlenecked on it rather than on utilization; Kata support if a design partner requires VM-grade isolation badly enough to eat the cold-start cost.