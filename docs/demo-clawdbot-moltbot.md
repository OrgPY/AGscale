# Demo Deployment Guide: clawdbot + moltbot (Official Images)

This demo deploys two agent services (`clawdbot`, `moltbot`) using official runtime images.

No local image build is required; official image overlays are provided.

## Profiles

- Base profile (default): official runtime with local memory/context/skill objects
- OpenClaw overlay (optional): official OpenClaw image (`openclaw/openclaw:latest`)
- OpenCode overlay (optional): BYO OpenCode endpoint/model on official runtime
- Moltbot-real overlay (optional): switches only `moltbot` to the official `openclaw/openclaw` runtime

## Prerequisites

- Kubernetes cluster reachable from your shell
- AGScale controller deployed
- AGScale CRDs installed

```bash
kubectl apply -k crds
```

## 1) Deploy base demo (immediate runnable)

```bash
kubectl apply -k examples/demo
```

This applies `examples/demo/base` and creates:

- `AgentRuntime/demo-real-agent-runtime` (official image `openclaw/openclaw:latest`)
- `AgentReliabilityPolicy/default-reliability`
- `Deployment/Service demo-httpbin` (local tool backend)
- `LLMEndpoint/local-ollama-endpoint` (local endpoint contract)
- `MemoryStore/local-memory` (local memory layer)
- `ContextProvider/local-context` (local context layer)
- `AgentSkill/httpbin-tool` (manual local skill)
- `Agent/clawdbot` + `Agent/moltbot`
- Budget policies (`default-namespace-budget`, `clawdbot-budget`, `moltbot-budget`)

## 2) Verify rollout

```bash
kubectl get agents
kubectl get deploy,svc,hpa | grep -E 'clawdbot|moltbot|demo-httpbin'
kubectl get agentskills
kubectl get llmendpoints,memorystores,contextproviders
kubectl get agentbudgetpolicies
```

## 3) Verify pod health and logs

```bash
kubectl get pods -l app.kubernetes.io/instance=clawdbot
kubectl get pods -l app.kubernetes.io/instance=moltbot
kubectl logs deploy/clawdbot --tail=100
kubectl logs deploy/moltbot --tail=100
```

## 4) Trigger budget exceeded (soft-limit)

```bash
kubectl annotate agent clawdbot \
  agscale.io/usage-updated-at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  agscale.io/usage-cost-usd-24h="999.99" \
  --overwrite
```

After ~60s:

```bash
kubectl get agent clawdbot -o jsonpath='{.status.budgetPhase}{" "}{.status.budgetReason}{"\n"}'
kubectl get events --field-selector involvedObject.kind=Agent,involvedObject.name=clawdbot
```

Expected:

- `budgetPhase=Exceeded`
- warning event emitted
- pod remains running (soft-limit)

## 5) Optional OpenClaw overlay (official image)

Create secret:

```bash
kubectl -n default create secret generic openclaw-api-key \
  --from-literal=apiKey='<YOUR_OPENCLAW_API_KEY>'
```

Apply overlay:

```bash
kubectl apply -k examples/demo/overlays/openclaw
```

This overlay uses the official image `openclaw/openclaw:latest` for both demo agents, so no local image build is needed for OpenClaw mode.

Verify runtime wiring:

```bash
kubectl get pod -l app.kubernetes.io/instance=clawdbot -o yaml | rg 'image:|containerPort: 18789'
kubectl get pod -l app.kubernetes.io/instance=moltbot -o yaml | rg 'image:|containerPort: 18789'
```

## 5b) Optional OpenCode overlay (BYO endpoint)

OpenCode setup is documented in `docs/demo-opencode-byovalue.md`.

## 6) Optional real Moltbot overlay

This overlay keeps default `clawdbot` behavior and switches only `moltbot` to a runtime with:

- gateway token from Kubernetes Secret
- local ephemeral state at `/data` (`emptyDir`)
- mounted config file from ConfigMap

Set your gateway token in the Secret manifest:

```bash
TOKEN="$(openssl rand -base64 32 | tr -d '\n')"
sed -i.bak "s/replace-with-secure-token/${TOKEN}/" examples/demo/overlays/moltbot-real/moltbot-gateway-secret.yaml
```

Apply overlay:

```bash
kubectl apply -k examples/demo/overlays/moltbot-real
```

Verify:

```bash
kubectl get agent moltbot -o yaml | rg "runtimeRef|readyReplicas|conditions"
kubectl get deploy,svc,secret,configmap | grep moltbot
kubectl get pod -l app.kubernetes.io/instance=moltbot -o yaml | rg "/data|/etc/moltbot|CLAWDBOT_GATEWAY_TOKEN"
```

Notes:

- This overlay uses `AgentRuntime.spec.volumes` + `volumeMounts` with `emptyDir` and config map mount.
- `env.valueFrom.secretKeyRef` is supported for secure runtime env injection.
- If you later revert to base profile:

```bash
kubectl apply -k examples/demo/base
```

## 7) Cleanup

```bash
kubectl delete -k examples/demo/overlays/moltbot-real --ignore-not-found
kubectl delete -k examples/demo/overlays/opencode --ignore-not-found
kubectl delete -k examples/demo/overlays/openclaw --ignore-not-found
kubectl delete -k examples/demo
```
