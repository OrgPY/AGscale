# Minimal Runnable Demo: OpenClaw + qwen-plus

This is the smallest AGScale demo that deploys one OpenClaw agent using model `qwen-plus`.
For the canonical operator flow (install order, troubleshooting, acceptance checklist),
see `docs/runbook-openclaw-operator.md`.

This is **gateway mode** (through OpenClaw runtime), not direct third-party mode.
For direct VectorEngine Qwen (`https://api.vectorengine.ai/v1`) with a single secret,
use `docs/demo-qwen-vectorengine-minimal.md`.

Direct mode quick step (single secret):

```bash
kubectl -n default create secret generic qwen-api-key \
  --from-literal=apiKey='<YOUR_VECTORENGINE_API_KEY>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

The manifest uses an in-cluster template endpoint by default:
`http://openclaw-gateway.default.svc.cluster.local:8080/v1`

## Prerequisites

- Kubernetes cluster access
- AGScale CRDs and controller installed

```bash
kubectl apply -k crds
helm upgrade --install agscale charts/agscale -n agscale-system --create-namespace \
  --set webhook.enabled=false \
  --set observability.enabled=false
```

If your cluster already has cert-manager and Prometheus Operator CRDs, you can use full features:

```bash
helm upgrade --install agscale charts/agscale -n agscale-system --create-namespace
```

## 1) Create required secrets

```bash
kubectl -n default create secret generic openclaw-api-key \
  --from-literal=apiKey='<YOUR_REAL_OR_PLACEHOLDER_API_KEY>' \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n default create secret generic openclaw-gateway \
  --from-literal=token='<ANY_SECURE_RANDOM_TOKEN>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

Notes:

- You can use placeholder values to validate Kubernetes object creation.
- For real model inference, use a real API key and a reachable OpenClaw gateway baseURL.
- Secret requirement split:
  - direct third-party Qwen mode: only `qwen-api-key`
  - OpenClaw gateway mode (this doc): `openclaw-api-key` + `openclaw-gateway`

## 2) Deploy minimal demo

```bash
kubectl apply -k examples/demo/minimal-openclaw-qwenplus
```

If your gateway URL is different, patch it before apply:

```bash
kubectl patch llmendpoint openclaw-qwenplus -n default --type merge \
  -p '{"spec":{"baseURL":"http://<YOUR_GATEWAY_HOST>:<PORT>/v1"}}'
```

## 3) Verify

```bash
kubectl get llmendpoint openclaw-qwenplus -o yaml | rg 'provider|defaultModel|baseURL'
kubectl get agent openclaw-qwenplus-agent
kubectl get deploy,svc | rg openclaw-qwenplus-agent
kubectl get pod -l app.kubernetes.io/instance=openclaw-qwenplus-agent -o yaml | rg 'image:|containerPort: 18789'
kubectl logs deploy/openclaw-qwenplus-agent --tail=100
```

Expected:

- `provider: openclaw`
- `defaultModel: qwen-plus`
- deployment/pod in `Running` state
- logs show successful upstream connectivity only when key/baseURL are real

## 4) Cleanup

```bash
kubectl delete -k examples/demo/minimal-openclaw-qwenplus --ignore-not-found
kubectl -n default delete secret openclaw-api-key openclaw-gateway --ignore-not-found
```
