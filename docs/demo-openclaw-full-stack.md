# OpenClaw Full-Stack Demo Walkthrough

This walkthrough deploys AGScale as an OpenClaw-focused stack with:

- OpenClaw LLM endpoints
- Skill hub at `https://clawhub.ai/`
- Agent deployment (`support-agent`)
- Team orchestration (`AgentWorkflow`, `AgentWorkflowRun`, `EventSource`, `IntegrationEndpoint`)
- CLI + TUI operations

For BYO OpenCode endpoint flow, see `docs/demo-opencode-byovalue.md`.

## 0) Prerequisites

- Kubernetes cluster access from your shell
- `kubectl`, `helm`, `docker` (or alternative image build/load flow)
- This repo checked out

Optional but recommended:

- `jq` and `rg` for easier verification

## 1) Install AGScale CRDs + Controller

From repo root:

```bash
kubectl apply -k crds
helm upgrade --install agscale charts/agscale -n agscale-system --create-namespace \
  --set webhook.enabled=false \
  --set observability.enabled=false
kubectl -n agscale-system rollout status deploy/agscale-controller-manager
```

If your cluster already has cert-manager and Prometheus Operator CRDs, you can use full features:

```bash
helm upgrade --install agscale charts/agscale -n agscale-system --create-namespace
```

## 2) Build and install AGScale CLI

```bash
make build-cli
./bin/agscale version
```

## 3) Create OpenClaw secrets

The platform manifests use `shared-openclaw` and `shared-openclaw-backup`.

```bash
kubectl -n default create secret generic openclaw-api-key \
  --from-literal=apiKey='<YOUR_OPENCLAW_API_KEY>' \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n default create secret generic openclaw-backup-api-key \
  --from-literal=apiKey='<YOUR_OPENCLAW_BACKUP_API_KEY>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

## 4) Deploy platform + agent + team objects

```bash
kubectl apply -k examples/platform
kubectl apply -k examples/agents
kubectl apply -k examples/team
```

Verify resources:

```bash
kubectl get llmendpoints
kubectl get skillregistries,agentskills
kubectl get agents
kubectl get agentworkflows,agentworkflowruns,eventsources,integrationendpoints,datagovernancepolicies
```

## 5) Run CLI health checks

```bash
./bin/agscale doctor -n default
./bin/agscale get agents -n default
./bin/agscale get workflows -n default
./bin/agscale get runs -n default
```

## 6) Trigger and inspect team orchestration

Render the workflow graph:

```bash
./bin/agscale team graph support-team -n default
```

Create a manual run:

```bash
./bin/agscale team run support-team -n default --trigger manual --correlation-id demo-openclaw-001
```

Get run status:

```bash
./bin/agscale get runs -n default
./bin/agscale team status <RUN_NAME_FROM_PREVIOUS_COMMAND> -n default
```

Notes:

- Workflow policy gates are enabled (tool/budget/data-governance checks).
- HTTP workflow steps are OpenClaw-compatible: controller tries `/invoke`, then `/chat` if `/invoke` is unavailable.

## 7) Use TUI for live operations

```bash
./bin/agscale tui agent support-agent -n default
```

Inside TUI, available actions:

- `r`: refresh
- `l`: show recent logs
- `c`: chat with the agent (`POST /chat`)
- `x`: restart one pod
- `s`: scale replicas
- `f`: fail over to backup LLM endpoint
- `q`: quit

## 8) Optional: Deploy clawdbot/moltbot demo with OpenClaw overlay (official image)

This path validates the two-agent demo profile.

Deploy base + OpenClaw overlay:

```bash
kubectl apply -k examples/demo
kubectl apply -k examples/demo/overlays/openclaw
```

Notes:

- OpenClaw overlay uses the official image `openclaw/openclaw:latest`.
- No local image build is required for this OpenClaw path.

Quick verify:

```bash
kubectl get agents | rg 'clawdbot|moltbot'
kubectl get llmendpoint shared-openclaw -o yaml | rg 'provider|baseURL|defaultModel'
```

## 9) Troubleshooting

1. `doctor` fails on CRDs

```bash
kubectl apply -k crds
```

2. `Agent` not ready due to missing LLM auth

```bash
kubectl -n default get secret openclaw-api-key openclaw-backup-api-key
```

3. Workflow run fails with policy deny

```bash
./bin/agscale team status <RUN_NAME> -n default
kubectl get agentworkflow support-team -o yaml | rg 'policyRefs'
```

4. Skill resolution issues

```bash
kubectl get agentskill web-search -o yaml | rg 'source|status|lastError'
```

## 10) Cleanup

```bash
kubectl delete -k examples/team --ignore-not-found
kubectl delete -k examples/agents --ignore-not-found
kubectl delete -k examples/platform --ignore-not-found
kubectl delete -k examples/demo/overlays/openclaw --ignore-not-found
kubectl delete -k examples/demo --ignore-not-found
helm uninstall agscale -n agscale-system
kubectl delete -k crds --ignore-not-found
```
