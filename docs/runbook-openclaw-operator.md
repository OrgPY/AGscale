# OpenClaw Operator Runbook (AGScale)

This is the primary deployment runbook for AGScale with OpenClaw-compatible agents.

It provides two validated paths:

1. Direct third-party Qwen mode (recommended first, minimal): one secret `qwen-api-key`
2. OpenClaw gateway mode: two secrets `openclaw-api-key` and `openclaw-gateway`

## 0) Prerequisites

- Kubernetes cluster access
- `kubectl`, `helm`
- Optional: `kind`, `jq`, `rg`

Recommended namespaces:

- system: `agscale-system`
- demo: `agscale-demo1` (or `default` for existing `minimal-openclaw-qwenplus` example)

## 1) Install order (critical)

### 1.1 Optional dependency CRDs for full chart features

If you plan to install AGScale chart with full defaults, your cluster must already include:

- cert-manager CRDs (`Certificate`, `Issuer`)
- Prometheus Operator CRDs (`ServiceMonitor`, `PrometheusRule`)

Quick check:

```bash
kubectl api-resources | rg 'cert-manager.io|monitoring.coreos.com'
```

If those CRDs are not present, use minimal AGScale chart mode first:

```bash
helm upgrade --install agscale charts/agscale -n agscale-system --create-namespace \
  --set webhook.enabled=false \
  --set observability.enabled=false
```

If your cluster already has those CRDs/controllers, use full mode:

```bash
helm upgrade --install agscale charts/agscale -n agscale-system --create-namespace
```

### 1.2 Install AGScale CRDs and verify controller

```bash
kubectl apply -k crds
kubectl -n agscale-system get pods
kubectl api-resources | rg agscale.io
```

Expected:

- agscale controller pod is `Running`
- AGScale CRDs (Agent, LLMEndpoint, AgentWorkflow, AgentWorkflowRun, etc.) appear

## 2) Mode A (recommended): Direct Qwen with VectorEngine/OpenAI-compatible API

Use this first because it is the smallest runnable path.

Important:

- For Qwen-compatible third-party endpoints, use `LLMEndpoint.spec.provider: openai`
- Do not set `provider: qwen`

### 2.1 Create namespace + secret

```bash
kubectl create namespace agscale-demo1 --dry-run=client -o yaml | kubectl apply -f -
kubectl -n agscale-demo1 create secret generic qwen-api-key \
  --from-literal=apiKey='<YOUR_VECTORENGINE_API_KEY>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

`qwen-api-key` value source: your third-party provider console/API key page.

### 2.2 Build/load demo image (if using kind)

```bash
docker build -t agscale/demo-agent:0.1.0 examples/demo/agent-app
kind load docker-image agscale/demo-agent:0.1.0 --name <YOUR_KIND_CLUSTER_NAME>
```

### 2.3 Deploy and verify

```bash
kubectl apply -k examples/demo/minimal-qwen-vectorengine
kubectl -n agscale-demo1 get llmendpoint qwen-vectorengine -o yaml | rg 'provider|baseURL|defaultModel'
kubectl -n agscale-demo1 get agent qwen-vectorengine-agent
kubectl -n agscale-demo1 get deploy,svc | rg qwen-vectorengine-agent
kubectl -n agscale-demo1 logs deploy/qwen-vectorengine-agent --tail=100
```

Expected:

- `provider: openai`
- `baseURL: https://api.vectorengine.ai/v1`
- `defaultModel: qwen-plus`
- Agent ready and deployment available

### 2.4 Chat verification (fallback-safe)

```bash
kubectl -n agscale-demo1 port-forward svc/qwen-vectorengine-agent 18080:80
```

In another terminal:

```bash
curl -s http://127.0.0.1:18080/healthz
curl -s -X POST http://127.0.0.1:18080/chat \
  -H 'Content-Type: application/json' \
  -d '{"message":"ping"}'
```

## 3) Mode B: OpenClaw gateway mode

Use when you need OpenClaw runtime/gateway path.

### 3.1 Create required secrets

```bash
kubectl -n default create secret generic openclaw-api-key \
  --from-literal=apiKey='<UPSTREAM_MODEL_API_KEY>' \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n default create secret generic openclaw-gateway \
  --from-literal=token='<ANY_SECURE_RANDOM_TOKEN>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

Secret provenance:

- `openclaw-api-key`: token OpenClaw uses to call the upstream model API
- `openclaw-gateway`: gateway auth token used by OpenClaw runtime itself

### 3.2 Deploy and verify

```bash
kubectl apply -k examples/demo/minimal-openclaw-qwenplus
kubectl -n default get llmendpoint openclaw-qwenplus -o yaml | rg 'provider|baseURL|defaultModel'
kubectl -n default get agent openclaw-qwenplus-agent
kubectl -n default logs deploy/openclaw-qwenplus-agent --tail=100
```

## 4) Team orchestration A2A validation

Apply the A2A sample:

```bash
kubectl apply -k examples/team-builder/openclaw-a2a
kubectl -n default get agentworkflow support-team-a2a -o yaml | rg 'protocol: a2a|integrationRef'
kubectl -n default get agentworkflowrun support-team-a2a-run-01
./bin/agscale team watch support-team-a2a-run-01 -n default
```

Expected:

- workflow accepted with `protocol: a2a`
- run transitions `Pending` -> `Running` -> `Succeeded` (or actionable failure details)

Negative drill (recommended):

- Change `integrationRef.name` to a non-existing object and re-apply
- Confirm run fails with clear reference/target resolution error

## 5) Troubleshooting matrix

### Helm install fails with missing CRD kinds

Symptom:

- `no matches for kind "Certificate"/"Issuer"/"ServiceMonitor"/"PrometheusRule"`

Cause:

- Cluster missing cert-manager/Prometheus Operator CRDs

Fix:

- Install dependency CRDs/controllers first, or install AGScale with:
  - `--set webhook.enabled=false --set observability.enabled=false`

### `LLMEndpoint ... provider: qwen` rejected

Symptom:

- validation error shows supported providers and rejects `qwen`

Cause:

- Third-party Qwen endpoint must use OpenAI-compatible provider setting

Fix:

- Set `spec.provider: openai`
- Keep `baseURL` as your Qwen-compatible endpoint URL

### `ReferencesResolved=False` with runtime/llm not found

Cause:

- referenced objects absent or wrong namespace/name

Fix:

- Create referenced resources first
- verify names and namespace on `runtimeRef` / `llmEndpointRef`

### Controller `ImagePullBackOff`

Cause:

- image tag/repository unavailable or auth issue

Fix:

- verify chart values/image repo/tag and pull secret configuration

### TUI chat DNS error on `*.svc.cluster.local`

Cause:

- running CLI outside cluster with no DNS route to cluster service

Fix:

- use namespace-correct agent target in TUI
- or use `kubectl port-forward` and local chat smoke test

## 6) Cleanup

Direct Qwen mode:

```bash
kubectl delete -k examples/demo/minimal-qwen-vectorengine --ignore-not-found
kubectl -n agscale-demo1 delete secret qwen-api-key --ignore-not-found
```

OpenClaw gateway mode:

```bash
kubectl delete -k examples/demo/minimal-openclaw-qwenplus --ignore-not-found
kubectl -n default delete secret openclaw-api-key openclaw-gateway --ignore-not-found
```

A2A team sample:

```bash
kubectl delete -k examples/team-builder/openclaw-a2a --ignore-not-found
```

## 7) Acceptance checklist

- [ ] AGScale CRDs installed and visible
- [ ] Controller is `Running` in `agscale-system`
- [ ] Mode A direct Qwen demo reaches ready state and chat smoke test passes
- [ ] Mode B OpenClaw gateway demo reaches ready state
- [ ] A2A workflow object applies with `protocol: a2a`
- [ ] A2A run produces expected final phase and step results
- [ ] Negative drill (bad `integrationRef`) returns actionable error
