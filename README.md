# agscale

`agscale` is a Kubernetes-native framework for deploying and scaling AI agents using custom resources.

It separates concerns into reusable platform objects:

- `Agent`: Deploy and scale a containerized agent workload.
- `AgentRuntime`: Define reusable runtime defaults (image, probes, resources, sidecars).
- `LLMEndpoint`: Centralize model provider config and credentials.
- `MemoryStore`: Define memory backends for short/long-term agent state.
- `ContextProvider`: Define context sources (Git, S3, HTTP, Kubernetes, vector DB, etc.).
- `AgentSkill`: Define reusable capabilities/tools that agents can call (manual or sourced from public hubs).
- `SkillRegistry` (optional scaffold): Future centralized registry governance layer.
- `AgentReliabilityPolicy`: Define SRE health thresholds and self-healing actions.
- `AgentBudgetPolicy`: Define soft budget limits for request/token/cost usage.

## Goals

- Make agent deployment as simple as creating a few YAML resources.
- Decouple runtime, model endpoint, memory, and context so teams can reuse them.
- Support scale via HPA-style controls and clean status reporting.

## Resource Model

1. Platform team creates shared `LLMEndpoint`, `MemoryStore`, `ContextProvider`, `AgentRuntime`.
2. App team creates `Agent` referencing those resources.
3. `agscale-controller` reconciles the `Agent` into `Deployment`/`Service` (and optional autoscaling).

See `docs/architecture.md` for details.

## Quick Start

```bash
kubectl apply -k crds/
kubectl apply -f examples/platform/
kubectl apply -f examples/agents/
```

Demo for two sample agents (`clawdbot`, `moltbot`):

- Guide: `docs/demo-clawdbot-moltbot.md`
- Manifests: `examples/demo/`

## Directory Layout

- `crds/`: CRD definitions and kustomization.
- `examples/platform/`: Shared platform resources.
- `examples/agents/`: Example agents using shared resources.
- `docs/`: Architecture and controller behavior.
- `api/`: Go API types and webhook validation/defaulting.
- `internal/controller/`: Reconciler implementation.
- `config/`: Operator deployment manifests (RBAC, manager, webhook).

## Operator Scaffold

This repository now includes a Go operator scaffold with:

- `Agent` reconciler:
  - Resolves references to runtime/model/memory/context/skill resources.
  - Reconciles `Deployment`, optional `Service`, and optional `HPA`.
  - Applies reliability policy checks and self-healing remediation actions.
  - Current concrete remediation actions: `RestartPods`, `ScaleOut`, `FailOpenReadOnly` (`TrafficDrain`/`RollbackRevision` are no-op placeholders in v1alpha1).
  - Publishes `Agent.status` conditions, ready replica count, and remediation history.
- `Agent` mutating/validating webhook:
  - Defaults replicas/service ports.
  - Defaults `reliabilityPolicyRef` to `default-reliability`.
  - Validates scale/service fields.
 - Performs cross-resource reference checks (`runtimeRef`, `reliabilityPolicyRef`, `llmEndpointRef`, `memoryRefs`, `contextRefs`, `skillRefs`).
 - Performs cross-resource reference checks (`runtimeRef`, `reliabilityPolicyRef`, `budgetPolicyRef`, `llmEndpointRef`, `memoryRefs`, `contextRefs`, `skillRefs`).

`AgentSkill` supports source-based resolution without hardcoding provider logic:

- `spec.source.protocol`: `mcp` or `openapi-http`
- `spec.source.url`: skill hub endpoint
- `spec.source.externalID`: external skill id
- `spec.syncPolicy.mode`: `Snapshot` or `TrackLatest`
- `spec.syncPolicy.intervalSeconds`: sync interval
- `Agent.spec.skillResolutionPolicy`: `Strict` (default) or `BestEffort`

`SkillRegistry` exists as an optional scaffold CRD for future centralized governance; direct `AgentSkill.spec.source` is the primary path in v1.

Observability pack supports Prometheus + Grafana + OTel hooks. Built-in alert rules are installed but inactive by default.

Budget policy is soft-limit in v1alpha1:

- `AgentBudgetPolicy` evaluates usage in rolling 24h windows.
- Exceeded budgets set status conditions and emit warning events; agent traffic is not blocked.
- Budget telemetry can be provided via agent annotations:
  - `agscale.io/usage-requests-24h`
  - `agscale.io/usage-tokens-24h`
  - `agscale.io/usage-cost-usd-24h`
  - `agscale.io/usage-updated-at` (RFC3339)

## Run Controller

```bash
go mod tidy
go run ./main.go
```

## Run Tests

```bash
go test ./...
```

`envtest`-based tests were added for:

- Controller reconcile behavior (`internal/controller/agent_controller_envtest_test.go`)
- Webhook defaulting and cross-resource validation (`api/v1alpha1/agent_webhook_envtest_test.go`)

Set `KUBEBUILDER_ASSETS` (or install envtest binaries) to run these tests locally.
Helper script:

```bash
eval "$(./hack/setup-envtest.sh)"
go test ./...
```

## Pre-commit Hook

Install:

```bash
./hack/install-pre-commit.sh
```

The hook runs:

- `gofmt` on staged Go files (and re-stages)
- `goimports` on staged Go files if installed
- `go vet ./...`
- `golangci-lint run` if installed

## Deploy Controller

```bash
kubectl apply -k config/default
```

Replace image in `config/manager/manager.yaml` with your pushed controller image before deploying in a real cluster.
`config/default` now includes cert-manager resources:

- `Issuer` + `Certificate` for webhook TLS
- CA injection annotations for mutating/validating webhook configurations
- Controller `Deployment` patch to mount generated cert secret

Install cert-manager CRDs/controller before applying `config/default`.

## CI

GitHub Actions workflow at `.github/workflows/ci.yaml` runs:

- `go vet ./...`
- `golangci-lint` (configured by `.golangci.yml`)
- `go mod tidy`
- envtest setup via `hack/setup-envtest.sh`
- `go test ./...`
- manifest rendering checks (`kubectl kustomize`)

## Helm Chart

Chart path: `charts/agscale`

Install:

```bash
helm install agscale charts/agscale -n agscale-system --create-namespace
```

Upgrade:

```bash
helm upgrade agscale charts/agscale -n agscale-system
```

Render locally:

```bash
helm template agscale charts/agscale
```

The chart includes:

- Controller `Deployment`, `ServiceAccount`, RBAC
- Default `AgentReliabilityPolicy` (`default-reliability`) configurable via values
- `AgentBudgetPolicy` support with controller-side budget evaluation metrics
- Optional observability resources (`ServiceMonitor`, `PrometheusRule`, Grafana dashboards)
- Built-in alert rules installed with inactive expressions by default (`vector(0)`), activatable via values
- Webhook `Service`, mutating/validating webhook configurations
- Optional cert-manager `Issuer` and `Certificate` (enabled by default)
- CRDs under `charts/agscale/crds`
