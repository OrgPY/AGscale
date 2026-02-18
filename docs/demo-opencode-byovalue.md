# OpenCode BYO Demo Walkthrough

This walkthrough deploys `clawdbot` and `moltbot` using your own OpenCode endpoint and model values.

The OpenCode overlay uses official image `openclaw/openclaw:latest`; no local image build is required.

## 0) Prerequisites

- Kubernetes cluster access
- `kubectl`, `kustomize`
- AGScale CRDs + controller installed

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

## 1) Create your OpenCode API key secret

```bash
kubectl -n default create secret generic opencode-api-key \
  --from-literal=apiKey='<YOUR_OPENCODE_API_KEY>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

## 2) Set your OpenCode endpoint + model

Edit `examples/demo/overlays/opencode/opencode-user-config.env`:

```env
OPENCODE_BASE_URL=https://your-real-opencode-endpoint/v1
OPENCODE_MODEL=your-real-opencode-model
```

## 3) Deploy base + OpenCode overlay

```bash
kubectl apply -k examples/demo
kubectl apply -k examples/demo/overlays/opencode
```

## 4) Verify resources

```bash
kubectl get agents
kubectl get llmendpoint shared-opencode -o yaml | rg 'provider|baseURL|defaultModel'
kubectl get deploy,svc | rg 'clawdbot|moltbot'
```

Expected:

- `LLMEndpoint/shared-opencode` has `provider: opencode`
- both agents reference `shared-opencode`
- agent env includes `AGENT_MODE=opencode`

## 5) Verify runtime wiring

```bash
kubectl get agents
kubectl get deploy,svc | rg 'clawdbot|moltbot'
kubectl get pod -l app.kubernetes.io/instance=clawdbot -o yaml | rg 'image:|containerPort: 18789'
```

## 6) Optional CLI scaffold helper

Print Secret + `LLMEndpoint` YAML template:

```bash
./bin/agscale opencode template \
  --namespace default \
  --name shared-opencode \
  --secret opencode-api-key \
  --base-url https://your-real-opencode-endpoint/v1 \
  --model your-real-opencode-model
```

## 7) Cleanup

```bash
kubectl delete -k examples/demo/overlays/opencode --ignore-not-found
kubectl delete -k examples/demo --ignore-not-found
kubectl -n default delete secret opencode-api-key --ignore-not-found
```
