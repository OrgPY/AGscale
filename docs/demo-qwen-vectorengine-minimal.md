# Minimal Runnable Demo: Direct Qwen (VectorEngine)

This is the smallest AGScale demo for direct third-party Qwen access at:
`https://api.vectorengine.ai/v1`

This mode needs only one secret: `qwen-api-key`.
It uses AGScale's demo runtime image (`agscale/demo-agent:0.1.0`) in OpenAI-compatible mode.
Important: for Qwen-compatible third-party endpoints, set `LLMEndpoint.spec.provider: openai`.
Do not use `provider: qwen` in this manifest.

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

## 1) Create required secret

`qwen-api-key` comes from your VectorEngine console/API key page.
This is the only secret required in direct mode.

```bash
kubectl create namespace agscale-demo1 --dry-run=client -o yaml | kubectl apply -f -
kubectl -n agscale-demo1 create secret generic qwen-api-key \
  --from-literal=apiKey='<YOUR_VECTORENGINE_API_KEY>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

## 2) Build and load the demo agent image

```bash
docker build -t agscale/demo-agent:0.1.0 examples/demo/agent-app
kind load docker-image agscale/demo-agent:0.1.0 --name <YOUR_KIND_CLUSTER_NAME>
```

Example cluster name:

```bash
kind get clusters
```

## 3) Deploy minimal demo

```bash
kubectl apply -k examples/demo/minimal-qwen-vectorengine
```

## 4) Verify

```bash
kubectl get llmendpoint qwen-vectorengine -o yaml | rg 'provider|defaultModel|baseURL'
kubectl get agent qwen-vectorengine-agent
kubectl get deploy,svc | rg qwen-vectorengine-agent
kubectl get pod -l app.kubernetes.io/instance=qwen-vectorengine-agent -o yaml | rg 'image:|containerPort: 8080'
kubectl logs deploy/qwen-vectorengine-agent --tail=100
```

Expected:

- `provider: openai`
- `baseURL: https://api.vectorengine.ai/v1`
- `defaultModel: qwen-plus`
- deployment/pod in `Running` state

Optional smoke test:

```bash
kubectl -n agscale-demo1 port-forward svc/qwen-vectorengine-agent 18080:80
```

Then in another shell:

```bash
curl -s http://127.0.0.1:18080/healthz
curl -s -X POST http://127.0.0.1:18080/chat \
  -H 'Content-Type: application/json' \
  -d '{"message":"ping"}'
```

## 5) Cleanup

```bash
kubectl delete -k examples/demo/minimal-qwen-vectorengine --ignore-not-found
kubectl -n agscale-demo1 delete secret qwen-api-key --ignore-not-found
```
