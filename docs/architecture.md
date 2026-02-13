# agscale Architecture (v1alpha1)

## Core Concepts

`agscale` treats an AI agent as a composition of five abstractions:

1. `Agent` (workload): desired replicas, traffic port, references to model/memory/context.
2. `AgentRuntime` (execution profile): default image, resources, probes, pod template data.
3. `LLMEndpoint` (model access): provider, base URL, auth secret, model/rate defaults.
4. `MemoryStore` (state): backend type and connection contract.
5. `ContextProvider` (knowledge): source type and refresh/access policy.
6. `AgentSkill` (capabilities): reusable skills/tools exposed to agents. Supports manual config or source-sync from skill hubs.
7. `SkillRegistry` (optional scaffold): centralized registry governance object for future multi-team use.
8. `AgentReliabilityPolicy` (SRE guardrails): SLO thresholds and remediation actions.
9. `AgentBudgetPolicy` (cost guardrails): rolling-window request/token/cost soft limits.

## API Group

- Group: `agscale.io`
- Version: `v1alpha1`
- Namespaced resources (initially)

## Reconciliation Contract

For each `Agent`:

1. Resolve references:
   - `runtimeRef` (required)
   - `reliabilityPolicyRef` (optional, defaults to `default-reliability`)
   - `budgetPolicyRef` (optional)
   - `llmEndpointRef` (optional)
   - `memoryRefs[]` (optional)
   - `contextRefs[]` (optional)
   - `skillRefs[]` (optional)
2. Render deployment template:
   - Base from `AgentRuntime.spec`.
   - Overlay `Agent.spec` overrides.
3. Inject runtime environment:
   - `AGSCALE_AGENT_NAME`
   - `AGSCALE_LLM_ENDPOINT`
   - Memory/context endpoint metadata from referenced objects.
   - `AGSCALE_SKILL_REFS`
   - `AGSCALE_SKILL_ENDPOINTS_JSON`
   - `AGSCALE_SKILL_CONFIG_JSON`
   - `AGSCALE_SKILL_PROTOCOLS_JSON`
4. Reconcile:
   - `Deployment` (required)
   - `Service` (if `service.enabled=true`)
   - `HorizontalPodAutoscaler` (if autoscaling configured)
   - Observability annotations/env for metrics and tracing (if enabled)
5. Publish status:
   - Conditions (`Ready`, `ReferencesResolved`, `DependencyHealthy`, `Healthy`, `ErrorBudgetBurning`, `Remediating`, `BudgetExceeded`)
   - `readyReplicas`, `serviceName`, `url`, `observedGeneration`
   - Remediation history (`lastRemediationAction`, `lastRemediationTime`, `recentRemediations`)
   - Budget projection fields (`budgetPhase`, `budgetReason`, `budgetUsageSnapshot`, `budgetLastEvaluatedTime`)

## Validation Rules

- `Agent.spec.runtimeRef` must be set.
- `Agent.spec.replicas` and autoscaling rules are mutually constrained.
- `LLMEndpoint.spec.auth.secretRef.name` required if `provider != local`.
- `MemoryStore.spec.connection.secretRef.name` required for managed backends.

## Suggested Controller Implementation

- Language: Go
- Stack:
  - `controller-runtime`
  - `kubebuilder` CRD/webhook generation
  - `envtest` integration tests

## Security Defaults

- No plaintext credentials in CRDs.
- Secrets referenced by name/key.
- Pod security context inherited from `AgentRuntime` with `Agent` override.
- Optional network policies for model/memory egress control.

## Observability Defaults

- Metrics enabled by default for controller and agent workloads.
- Built-in Prometheus alert rules are installed but inactive by default.
- Alert activation is controlled via Helm values.
