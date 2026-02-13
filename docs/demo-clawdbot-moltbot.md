# Demo Deployment Guide: clawdbot + moltbot

This guide deploys two sample agents (`clawdbot`, `moltbot`) on AGScale with reliability and budget policies.

## What this validates

- `Agent` reconciliation to `Deployment` + `Service` + optional `HPA`
- `AgentSkill` reference injection
- `AgentBudgetPolicy` soft-limit evaluation and status projection
- Built-in alert rules present (inactive by default unless activated in Helm values)

## Prerequisites

- Kubernetes cluster reachable from your shell
- AGScale controller deployed
- AGScale CRDs installed
- Secrets created in `default` namespace:

```bash
kubectl -n default create secret generic openai-api-key --from-literal=apiKey='<your-openai-key>'
kubectl -n default create secret generic redis-auth --from-literal=password='<redis-password>'
kubectl -n default create secret generic git-access-token --from-literal=token='<git-token>'
```

## 1) Deploy demo resources

```bash
kubectl apply -k examples/demo
```

This applies:

- Shared platform objects from `examples/platform/*`
- Agents: `clawdbot`, `moltbot`
- Agent budgets: `clawdbot-budget`, `moltbot-budget`

## 2) Verify controller results

```bash
kubectl get agents
kubectl get deploy,svc,hpa | grep -E 'clawdbot|moltbot'
kubectl get agentbudgetpolicies
```

Check status projection:

```bash
kubectl get agent clawdbot -o jsonpath='{.status.budgetPhase}{"\n"}'
kubectl get agent moltbot -o jsonpath='{.status.budgetPhase}{"\n"}'
```

Check policy status:

```bash
kubectl get agentbudgetpolicy clawdbot-budget -o yaml
kubectl get agentbudgetpolicy moltbot-budget -o yaml
```

## 3) Trigger a budget warning (soft-limit)

Patch clawdbot usage above budget:

```bash
kubectl annotate agent clawdbot \
  agscale.io/usage-updated-at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  agscale.io/usage-cost-usd-24h="999.99" \
  --overwrite
```

Then verify:

```bash
kubectl get agent clawdbot -o jsonpath='{.status.budgetPhase}{" "}{.status.budgetReason}{"\n"}'
kubectl get events --field-selector involvedObject.kind=Agent,involvedObject.name=clawdbot
```

Expected: `budgetPhase=Exceeded` with warning event; workload remains running (soft-limit behavior).

## 4) Optional: activate built-in budget alerts

By default alert expressions are `vector(0)` (installed, inactive).
Activate globally or per-rule in Helm values:

- `observability.alerting.active=true`, or
- `observability.alerting.rules.budget_exceeded.active=true`
- `observability.alerting.rules.budget_telemetry_stale.active=true`

## 5) Cleanup

```bash
kubectl delete -k examples/demo
```

## Notes

- Demo images are placeholders:
  - `ghcr.io/example/clawdbot:0.1.0`
  - `ghcr.io/example/moltbot:0.1.0`
  Replace with your real images before running in a real cluster.
- If your cluster does not have `redis.default.svc.cluster.local:6379`, update `examples/platform/memorystore-redis.yaml`.
