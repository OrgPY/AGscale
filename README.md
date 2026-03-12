# agscale

`agscale` is a Kubernetes-native framework for deploying and scaling AI agents using custom resources.

Current project focus is OpenClaw (`openclaw`/`openClawd`) compatibility.

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
- `AgentToolPolicy`: Define tool/skill governance (allowlists, protocols, approval mode).

## Goals

- Make agent deployment as simple as creating a few YAML resources.
- Decouple runtime, model endpoint, memory, and context so teams can reuse them.
- Support scale via HPA-style controls and clean status reporting.
- Support runtime storage wiring via `volumes` and `volumeMounts` in runtime/agent specs.
- Support secure env injection via `env.valueFrom` (e.g. `secretKeyRef`) in runtime/agent specs.

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

- Primary operator runbook (install order + minimal demos + troubleshooting): `docs/runbook-openclaw-operator.md`
- Guide: `docs/demo-clawdbot-moltbot.md`
- Full OpenClaw stack walkthrough: `docs/demo-openclaw-full-stack.md`
- Minimal Direct Qwen (VectorEngine): `docs/demo-qwen-vectorengine-minimal.md`
- Minimal OpenClaw Gateway + qwen-plus: `docs/demo-openclaw-minimal-qwenplus.md`
- Direct Qwen mode needs only one secret: `qwen-api-key` (see direct minimal doc).
- OpenCode BYO walkthrough: `docs/demo-opencode-byovalue.md`
- Team builder quickstart: `docs/team-builder-quickstart.md`
- Fleet command center (local/SSH discovery + task scheduling): `docs/fleet-command-center.md`
- Manifests: `examples/demo/base/` (default), `examples/demo/overlays/openclaw/` (optional), `examples/demo/overlays/opencode/` (optional, BYO endpoint), `examples/demo/overlays/moltbot-real/` (optional)
- Team builder static demo: `examples/team-builder/openclaw-basic/`
- Real demo app source: `examples/demo/agent-app/`
- Base demo now includes local `LLMEndpoint`, `MemoryStore`, and `ContextProvider` resources for full-stack composition.
- Demo overlays focus on explicit provider selection (OpenClaw/OpenCode) and `moltbot-real`.
- OpenClaw overlay uses official image `openclaw/openclaw:latest` (no local image build).

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
  - Current concrete remediation actions: `RestartPods`, `ScaleOut`, `SwitchToBackupLLM`, `FailOpenReadOnly` (`TrafficDrain`/`RollbackRevision` are no-op placeholders in v1alpha1).
  - Dependency circuit breaker support: repeated dependency failures can open a temporary circuit and pause aggressive retries.
  - Backup LLM failover: set `metadata.annotations.agscale.io/backup-llm-endpoint` on `Agent` and include `SwitchToBackupLLM` in remediation actions.
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

Default skill hub for examples is `https://clawhub.ai/`.

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

## CLI

Build:

```bash
make build-cli
```

Common commands:

```bash
./bin/agscale cluster get agents -n default
./bin/agscale cluster doctor -n default
./bin/agscale cluster describe agent support-agent -n default
./bin/agscale cluster logs agent support-agent -n default --tail 100
./bin/agscale cluster apply -k examples/platform
./bin/agscale team run support-team -n default
./bin/agscale team graph support-team -n default
./bin/agscale team status support-team-run-<timestamp> -n default
./bin/agscale team init support-team-easy -n default --profile openclaw --out support-team-easy.yaml
./bin/agscale team up support-team-easy -n default --profile openclaw --out support-team-easy.yaml
./bin/agscale team command-center incident-401 -n default --selector 'agscale.io/domain=support' --require-skills web-search --max-agents 3 --topology dag --apply --run
./bin/agscale team validate -f support-team-easy.yaml -n default
./bin/agscale team watch support-team-easy-run-<timestamp> -n default
./bin/agscale fleet scan --local --scan-mode agent-aware --write
./bin/agscale fleet list --status healthy
./bin/agscale fleet run incident-401 --selector 'scope=local' --max-agents 4 --topology dag --explain-selection --dry-run
./bin/agscale tui
./bin/agscale cluster tui agent support-agent -n default
./bin/agscale opencode template --base-url https://your-opencode-endpoint.example/v1 --model your-opencode-model
```

TUI notes:

- `tui` opens a combined selector for cluster `Agent` resources and fleet inventory agents.
- `cluster tui agent <name>` keeps the Kubernetes operator view with status, logs, chat, restart-pod, scale, and backup-LLM failover actions.
- Chat uses `Agent.status.url` and defaults to `POST /chat`.
- Override chat path with annotation `agscale.io/chat-path`.

Workflow run notes:

- `AgentWorkflow.spec.steps[*].dependsOn` enables DAG dependencies.
- `AgentWorkflow.spec.steps[*].protocol` supports `http`, `mcp`, or `a2a`.
- `AgentWorkflow.spec.steps[*].integrationRef` can target an `IntegrationEndpoint` (optional, primarily for `a2a`).
- `AgentWorkflow.spec.steps[*].endpointPath` can override default invoke path (`/invoke`, `/mcp/invoke`, or `/a2a/invoke`).
- For HTTP workflow steps, controller tries `/invoke` then `/chat` by default for OpenClaw compatibility.
- `AgentWorkflow.spec.policyRefs.toolPolicyRef` gates allowed protocols and optional human approval.
- `AgentWorkflow.spec.policyRefs.budgetPolicyRef` blocks runs when budget status is `Exceeded`.
- `AgentWorkflow.spec.policyRefs.dataGovernancePolicyRef` enforces destination allow-list and payload redaction.

Event and integration notes:

- `IntegrationEndpoint` controller performs health checks and updates readiness.
- `EventSource` controller can trigger `AgentWorkflowRun` objects on interval (annotation `agscale.io/interval-seconds`).

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

## CLI Release

CLI-only release pipeline:

- GoReleaser config: `.goreleaser-cli.yaml`
- GitHub Actions workflow: `.github/workflows/release-cli.yaml`

Publish a new CLI release by tagging:

```bash
git tag v0.1.2
git push origin v0.1.2
```

The workflow builds `./cmd/agscale` for linux/darwin/windows (amd64/arm64) and publishes artifacts to GitHub Releases.
It publishes both compressed archives and raw executable files (`format: binary`).

## NPM / NPX

NPM wrapper package path: `npm/` (package: `@agscale/agscale`).

Run directly without install:

```bash
npx @agscale/agscale version
```

If package version is not yet aligned with release tag, pin release tag:

```bash
AGSCALE_RELEASE_TAG=v0.1.2 npx @agscale/agscale version
```

NPM publish workflow: `.github/workflows/publish-npm.yaml` (Trusted Publishing via GitHub OIDC, no `NPM_TOKEN` needed).

Headless Linux install (no UI):

```bash
curl -fsSL https://raw.githubusercontent.com/OrgPY/AGscale/main/hack/install-cli.sh | bash
agscale version
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/OrgPY/AGscale/main/hack/install-cli.sh | AGSCALE_VERSION=v0.1.2 bash
```

## Helm Chart

Chart path: `charts/agscale`

Install (minimal mode, no cert-manager/Prometheus CRDs required):

```bash
helm upgrade --install agscale charts/agscale -n agscale-system --create-namespace \
  --set webhook.enabled=false \
  --set observability.enabled=false
```

Install (full mode, requires cert-manager + Prometheus Operator CRDs):

```bash
helm upgrade --install agscale charts/agscale -n agscale-system --create-namespace
```

Upgrade (minimal mode):

```bash
helm upgrade agscale charts/agscale -n agscale-system \
  --set webhook.enabled=false \
  --set observability.enabled=false
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

Common install error:

- If you see `no matches for kind "Certificate"/"Issuer"/"ServiceMonitor"/"PrometheusRule"`, either:
  - install cert-manager and Prometheus Operator CRDs, or
  - use minimal mode flags: `--set webhook.enabled=false --set observability.enabled=false`
