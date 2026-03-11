# agscale Architecture (v1alpha1)

## Core Concepts

`agscale` treats an AI agent as a composition of five abstractions:

1. `Agent` (workload): desired replicas, traffic port, references to model/memory/context.
2. `AgentRuntime` (execution profile): default image, resources, probes, pod template data, volumes, and mounts.
3. `LLMEndpoint` (model access): provider, base URL, auth secret, model/rate defaults.
4. `MemoryStore` (state): backend type and connection contract.
5. `ContextProvider` (knowledge): source type and refresh/access policy.
6. `AgentSkill` (capabilities): reusable skills/tools exposed to agents. Supports manual config or source-sync from skill hubs.
7. `SkillRegistry` (optional scaffold): centralized registry governance object for future multi-team use.
8. `AgentReliabilityPolicy` (SRE guardrails): SLO thresholds and remediation actions.
9. `AgentBudgetPolicy` (cost guardrails): rolling-window request/token/cost soft limits.
10. `AgentToolPolicy` (tool guardrails): allowed skills/protocols and optional human-gate approval.

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
   - Merge `volumes` and `volumeMounts` by name (agent values override runtime values).
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

For `AgentWorkflow`/`AgentWorkflowRun`:

1. `AgentWorkflow` reconciler validates and marks workflow ready.
2. `AgentWorkflowRun` reconciler resolves target workflow, executes steps with dependency-aware DAG scheduling, and writes per-step result status.
3. Step dependencies come from `steps[].dependsOn`; if omitted, the controller falls back to sequential chaining.
4. Each step invokes referenced agent endpoint using protocol-aware routing:
   - `protocol: http` -> default `/invoke`
     fallback `/chat` for OpenClaw-compatible agents
   - `protocol: mcp` -> default `/mcp/invoke`
   - `protocol: a2a` -> default `/a2a/invoke` (or `integrationRef` target URL)
   - `endpointPath` can override both defaults.
5. Output from successful dependency steps is passed into the next ready step as input.
6. Policy gating during run execution:
   - `toolPolicyRef`: protocol allow-list and optional human approval check.
   - `budgetPolicyRef`: deny run when referenced budget policy is `Exceeded`.
   - `dataGovernancePolicyRef`: destination allow-list + lightweight PII redaction before invoke.

For `IntegrationEndpoint`:

1. Controller probes `spec.connection.url` on a fixed interval.
2. Updates `status.ready`, `status.lastError`, and readiness condition.

For `EventSource`:

1. Controller validates target workflow and emits `AgentWorkflowRun` objects periodically.
2. Trigger cadence defaults to 300s and can be set with annotation `agscale.io/interval-seconds`.

## AI-SRE Reliability Techniques Implemented

- Dependency circuit breaker:
  - Tracks repeated dependency resolution failures (LLM/memory/context/skill).
  - Uses `AgentReliabilityPolicy.spec.dependencyChecks.*.consecutiveFailuresForOpen` as threshold.
  - Opens a temporary circuit (duration follows `health.failureWindowSeconds`) to avoid tight failure loops.
- Graceful degraded mode:
  - `FailOpenReadOnly` remediation annotates the workload for read-only degraded serving.
- LLM provider failover:
  - `SwitchToBackupLLM` remediation can switch `Agent.spec.llmEndpointRef` to a backup endpoint.
  - Backup target is configured via `Agent.metadata.annotations["agscale.io/backup-llm-endpoint"]`.

## Validation Rules

- `Agent.spec.runtimeRef` must be set.
- `Agent.spec.replicas` and autoscaling rules are mutually constrained.
- `LLMEndpoint.spec.auth.secretRef.name` required for authenticated providers (for example `openclaw`, `opencode`, `openai`); not required for local providers like `ollama`/`vllm`.
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
