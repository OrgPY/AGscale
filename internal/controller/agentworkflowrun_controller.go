package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

type AgentWorkflowRunReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *AgentWorkflowRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	started := time.Now()
	result := "success"
	defer func() {
		observeReconcile("AgentWorkflowRun", result, started)
	}()

	run := &agscalev1alpha1.AgentWorkflowRun{}
	if err := r.Get(ctx, req.NamespacedName, run); err != nil {
		result = "not_found"
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if run.Status.Phase == "Succeeded" || run.Status.Phase == "Failed" {
		return ctrl.Result{}, nil
	}

	workflow := &agscalev1alpha1.AgentWorkflow{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: run.Namespace, Name: run.Spec.WorkflowRef.Name}, workflow); err != nil {
		return r.failRun(ctx, run, "_resolve_workflow", err)
	}

	now := metav1.Now()
	run.Status.ObservedGeneration = run.Generation
	if run.Status.StartedAt == nil {
		run.Status.StartedAt = &now
	}
	run.Status.Phase = "Running"
	if err := r.Status().Update(ctx, run); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("AgentWorkflowRun", "StatusUpdateRunning").Inc()
		return ctrl.Result{}, err
	}

	steps, err := normalizeWorkflowSteps(workflow.Spec.Steps)
	if err != nil {
		return r.failRun(ctx, run, "_normalize_steps", err)
	}
	if len(steps) == 0 {
		return r.failRun(ctx, run, "_normalize_steps", fmt.Errorf("workflow has no steps"))
	}

	policyDecisions, governancePolicy, err := r.evaluateWorkflowPolicies(ctx, run, workflow, steps)
	run.Status.PolicyDecisions = policyDecisions
	if err := r.Status().Update(ctx, run); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("AgentWorkflowRun", "StatusUpdatePolicy").Inc()
		return ctrl.Result{}, err
	}
	if err != nil {
		return r.failRun(ctx, run, "_policy_gate", err)
	}

	outputs := map[string]map[string]any{}
	statusByStep := map[string]string{}
	resultsByStep := map[string]agscalev1alpha1.WorkflowRunStepResult{}
	completed := map[string]bool{}

	for len(completed) < len(steps) {
		progress := false
		for _, step := range steps {
			if completed[step.Name] {
				continue
			}
			depsReady := true
			for _, dep := range step.DependsOn {
				if !completed[dep] {
					depsReady = false
					break
				}
				if statusByStep[dep] != "Succeeded" {
					statusByStep[step.Name] = "Skipped"
					completed[step.Name] = true
					resultsByStep[step.Name] = agscalev1alpha1.WorkflowRunStepResult{
						StepName: step.Name,
						Status:   "Skipped",
						Error:    fmt.Sprintf("dependency %q did not succeed", dep),
					}
					progress = true
					break
				}
			}
			if completed[step.Name] {
				continue
			}
			if !depsReady {
				continue
			}
			if governancePolicy != nil && len(governancePolicy.Spec.AllowedDestinations) > 0 {
				target := ""
				if step.AgentRef != nil {
					target = step.AgentRef.Name
				}
				if !containsString(governancePolicy.Spec.AllowedDestinations, target) {
					resultsByStep[step.Name] = agscalev1alpha1.WorkflowRunStepResult{
						StepName: step.Name,
						Status:   "Failed",
						Error:    fmt.Sprintf("destination %q not allowed by DataGovernancePolicy %q", target, governancePolicy.Name),
					}
					run.Status.StepResults = orderedResults(steps, resultsByStep)
					_ = r.Status().Update(ctx, run)
					return r.failRunWithExisting(ctx, run)
				}
			}

			input := mergeDependencyOutputs(step.DependsOn, outputs)
			if governancePolicy != nil {
				redactPayloadMap(input, governancePolicy.Spec.PIIRedaction)
			}
			stepResult, out, err := r.executeStep(ctx, run, workflow, step, input)
			if err != nil {
				stepResult.Status = "Failed"
				stepResult.Error = err.Error()
				resultsByStep[step.Name] = stepResult
				statusByStep[step.Name] = "Failed"
				completed[step.Name] = true
				run.Status.StepResults = orderedResults(steps, resultsByStep)
				if err := r.Status().Update(ctx, run); err != nil {
					result = "error"
					reconcileErrorsTotal.WithLabelValues("AgentWorkflowRun", "StatusUpdateStep").Inc()
					return ctrl.Result{}, err
				}
				return r.failRunWithExisting(ctx, run)
			}
			outputs[step.Name] = out
			resultsByStep[step.Name] = stepResult
			statusByStep[step.Name] = "Succeeded"
			completed[step.Name] = true
			progress = true
			run.Status.StepResults = orderedResults(steps, resultsByStep)
			if err := r.Status().Update(ctx, run); err != nil {
				result = "error"
				reconcileErrorsTotal.WithLabelValues("AgentWorkflowRun", "StatusUpdateStep").Inc()
				return ctrl.Result{}, err
			}
		}
		if !progress {
			for _, s := range steps {
				if !completed[s.Name] {
					resultsByStep[s.Name] = agscalev1alpha1.WorkflowRunStepResult{
						StepName: s.Name,
						Status:   "Failed",
						Error:    "workflow dependency deadlock or cycle detected",
					}
					completed[s.Name] = true
				}
			}
			run.Status.StepResults = orderedResults(steps, resultsByStep)
			if err := r.Status().Update(ctx, run); err != nil {
				result = "error"
				return ctrl.Result{}, err
			}
			return r.failRunWithExisting(ctx, run)
		}
	}

	done := metav1.Now()
	run.Status.Phase = "Succeeded"
	run.Status.CompletedAt = &done
	if err := r.Status().Update(ctx, run); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("AgentWorkflowRun", "StatusUpdateDone").Inc()
		return ctrl.Result{}, err
	}
	workflowRunsTotal.WithLabelValues("succeeded").Inc()

	workflowLatest := &agscalev1alpha1.AgentWorkflow{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: workflow.Namespace, Name: workflow.Name}, workflowLatest); err == nil {
		workflowLatest.Status.ObservedGeneration = workflowLatest.Generation
		workflowLatest.Status.LastRunID = run.Name
		workflowLatest.Status.LastRunTime = &done
		workflowLatest.Status.Phase = "Ready"
		workflowLatest.Status.RunCount = workflowLatest.Status.RunCount + 1
		_ = r.Status().Update(ctx, workflowLatest)
	}

	return ctrl.Result{}, nil
}

func (r *AgentWorkflowRunReconciler) evaluateWorkflowPolicies(ctx context.Context, run *agscalev1alpha1.AgentWorkflowRun, workflow *agscalev1alpha1.AgentWorkflow, steps []agscalev1alpha1.WorkflowStepSpec) ([]agscalev1alpha1.WorkflowRunPolicyDecision, *agscalev1alpha1.DataGovernancePolicy, error) {
	decisions := make([]agscalev1alpha1.WorkflowRunPolicyDecision, 0, 4)
	var governancePolicy *agscalev1alpha1.DataGovernancePolicy

	if ref := workflow.Spec.PolicyRefs.ToolPolicyRef; ref != nil && ref.Name != "" {
		policy := &agscalev1alpha1.AgentToolPolicy{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: run.Namespace, Name: ref.Name}, policy); err != nil {
			decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "AgentToolPolicy", Result: "Deny", Reason: err.Error()})
			return decisions, nil, fmt.Errorf("tool policy resolve failed: %w", err)
		}
		if policy.Spec.ApprovalMode == "HumanGate" && run.Annotations["agscale.io/tool-approval"] != "approved" {
			decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "AgentToolPolicy", Result: "Deny", Reason: "human approval required"})
			return decisions, nil, fmt.Errorf("tool policy requires approval annotation agscale.io/tool-approval=approved")
		}
		if len(policy.Spec.AllowedProtocols) > 0 {
			for _, step := range steps {
				if !containsString(policy.Spec.AllowedProtocols, string(step.Protocol)) {
					decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "AgentToolPolicy", Result: "Deny", Reason: fmt.Sprintf("protocol %s not allowed for step %s", step.Protocol, step.Name)})
					return decisions, nil, fmt.Errorf("tool policy denied protocol %q for step %q", step.Protocol, step.Name)
				}
			}
		}
		decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "AgentToolPolicy", Result: "Allow", Reason: "protocol and approval checks passed"})
	}

	if ref := workflow.Spec.PolicyRefs.BudgetPolicyRef; ref != nil && ref.Name != "" {
		budget := &agscalev1alpha1.AgentBudgetPolicy{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: run.Namespace, Name: ref.Name}, budget); err != nil {
			decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "AgentBudgetPolicy", Result: "Deny", Reason: err.Error()})
			return decisions, nil, fmt.Errorf("budget policy resolve failed: %w", err)
		}
		if budget.Status.Phase == agscalev1alpha1.BudgetPhaseExceeded {
			decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "AgentBudgetPolicy", Result: "Deny", Reason: "budget exceeded"})
			return decisions, nil, fmt.Errorf("budget policy %q is exceeded", budget.Name)
		}
		decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "AgentBudgetPolicy", Result: "Allow", Reason: string(budget.Status.Phase)})
	}

	if ref := workflow.Spec.PolicyRefs.DataGovernancePolicyRef; ref != nil && ref.Name != "" {
		policy := &agscalev1alpha1.DataGovernancePolicy{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: run.Namespace, Name: ref.Name}, policy); err != nil {
			decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "DataGovernancePolicy", Result: "Deny", Reason: err.Error()})
			return decisions, nil, fmt.Errorf("data governance policy resolve failed: %w", err)
		}
		if containsString(policy.Spec.ForbiddenFields, "correlationID") && run.Spec.CorrelationID != "" {
			decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "DataGovernancePolicy", Result: "Deny", Reason: "correlationID forbidden"})
			return decisions, nil, fmt.Errorf("data governance policy %q forbids correlationID", policy.Name)
		}
		decisions = append(decisions, agscalev1alpha1.WorkflowRunPolicyDecision{Policy: "DataGovernancePolicy", Result: "Allow", Reason: "governance checks passed"})
		governancePolicy = policy
	}

	return decisions, governancePolicy, nil
}

func (r *AgentWorkflowRunReconciler) failRun(ctx context.Context, run *agscalev1alpha1.AgentWorkflowRun, stepName string, err error) (ctrl.Result, error) {
	run.Status.StepResults = append(run.Status.StepResults, agscalev1alpha1.WorkflowRunStepResult{
		StepName: stepName,
		Status:   "Failed",
		Error:    err.Error(),
	})
	return r.failRunWithExisting(ctx, run)
}

func (r *AgentWorkflowRunReconciler) failRunWithExisting(ctx context.Context, run *agscalev1alpha1.AgentWorkflowRun) (ctrl.Result, error) {
	now := metav1.Now()
	run.Status.ObservedGeneration = run.Generation
	run.Status.Phase = "Failed"
	run.Status.CompletedAt = &now
	if err := r.Status().Update(ctx, run); err != nil {
		reconcileErrorsTotal.WithLabelValues("AgentWorkflowRun", "FailRunStatusUpdate").Inc()
		return ctrl.Result{}, err
	}
	workflowRunsTotal.WithLabelValues("failed").Inc()
	return ctrl.Result{}, nil
}

func normalizeWorkflowSteps(in []agscalev1alpha1.WorkflowStepSpec) ([]agscalev1alpha1.WorkflowStepSpec, error) {
	steps := make([]agscalev1alpha1.WorkflowStepSpec, len(in))
	copy(steps, in)
	if len(steps) == 0 {
		return steps, nil
	}
	seen := map[string]bool{}
	for i := range steps {
		if steps[i].Name == "" {
			return nil, fmt.Errorf("workflow step at index %d has empty name", i)
		}
		if seen[steps[i].Name] {
			return nil, fmt.Errorf("duplicate workflow step name %q", steps[i].Name)
		}
		seen[steps[i].Name] = true
		if len(steps[i].DependsOn) == 0 && i > 0 {
			steps[i].DependsOn = []string{steps[i-1].Name}
		}
		for _, dep := range steps[i].DependsOn {
			if dep == steps[i].Name {
				return nil, fmt.Errorf("step %q cannot depend on itself", steps[i].Name)
			}
		}
		if steps[i].Protocol == "" {
			if steps[i].Type == agscalev1alpha1.WorkflowStepToolCall {
				steps[i].Protocol = agscalev1alpha1.WorkflowProtocolMCP
			} else {
				steps[i].Protocol = agscalev1alpha1.WorkflowProtocolHTTP
			}
		}
	}
	for _, s := range steps {
		for _, dep := range s.DependsOn {
			if !seen[dep] {
				return nil, fmt.Errorf("step %q depends on unknown step %q", s.Name, dep)
			}
		}
	}
	return steps, nil
}

func mergeDependencyOutputs(deps []string, outputs map[string]map[string]any) map[string]any {
	if len(deps) == 0 {
		return map[string]any{}
	}
	merged := map[string]any{"deps": map[string]any{}}
	depMap := merged["deps"].(map[string]any)
	for _, dep := range deps {
		if out, ok := outputs[dep]; ok {
			depMap[dep] = out
		}
	}
	return merged
}

func orderedResults(steps []agscalev1alpha1.WorkflowStepSpec, byName map[string]agscalev1alpha1.WorkflowRunStepResult) []agscalev1alpha1.WorkflowRunStepResult {
	out := make([]agscalev1alpha1.WorkflowRunStepResult, 0, len(byName))
	for _, s := range steps {
		if r, ok := byName[s.Name]; ok {
			out = append(out, r)
		}
	}
	return out
}

func redactPayloadMap(payload map[string]any, fields []string) {
	if len(payload) == 0 || len(fields) == 0 {
		return
	}
	fieldSet := map[string]bool{}
	for _, f := range fields {
		fieldSet[f] = true
	}
	for k, v := range payload {
		if depMap, ok := v.(map[string]any); ok {
			for depK, depV := range depMap {
				if nested, ok := depV.(map[string]any); ok {
					for nk := range nested {
						if fieldSet[nk] {
							nested[nk] = "[REDACTED]"
						}
					}
					depMap[depK] = nested
				}
			}
			payload[k] = depMap
		}
	}
}

func (r *AgentWorkflowRunReconciler) executeStep(ctx context.Context, run *agscalev1alpha1.AgentWorkflowRun, workflow *agscalev1alpha1.AgentWorkflow, step agscalev1alpha1.WorkflowStepSpec, input map[string]any) (agscalev1alpha1.WorkflowRunStepResult, map[string]any, error) {
	stepStarted := time.Now()
	result := agscalev1alpha1.WorkflowRunStepResult{StepName: step.Name, Status: "Succeeded"}
	if step.AgentRef == nil || step.AgentRef.Name == "" {
		return result, nil, fmt.Errorf("step missing agentRef")
	}

	agent := &agscalev1alpha1.Agent{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: run.Namespace, Name: step.AgentRef.Name}, agent); err != nil {
		return result, nil, fmt.Errorf("resolve agent: %w", err)
	}
	if agent.Status.URL == "" {
		return result, nil, fmt.Errorf("agent url unavailable")
	}

	payload := map[string]any{
		"runId":    run.Name,
		"workflow": workflow.Name,
		"step":     step.Name,
		"from":     workflow.Spec.EntryAgentRef.Name,
		"to":       step.AgentRef.Name,
		"protocol": step.Protocol,
		"input":    input,
	}
	body, _ := json.Marshal(payload)
	attempts := int(step.RetryPolicy.MaxRetries) + 1
	if attempts < 1 {
		attempts = 1
	}
	endpoints := resolveStepEndpoints(agent.Status.URL, step)
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		for _, endpoint := range endpoints {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
			if err != nil {
				lastErr = err
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			httpClient := &http.Client{Timeout: stepTimeout(step.TimeoutSeconds)}
			resp, err := httpClient.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode < 300 {
				parsed := map[string]any{}
				if len(respBody) > 0 {
					if err := json.Unmarshal(respBody, &parsed); err != nil {
						parsed["raw"] = string(respBody)
					}
				}
				result.DurationMs = time.Since(stepStarted).Milliseconds()
				return result, parsed, nil
			}
			lastErr = fmt.Errorf("endpoint %s returned http %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
		if attempt < attempts && step.RetryPolicy.BackoffSeconds > 0 {
			time.Sleep(time.Duration(step.RetryPolicy.BackoffSeconds) * time.Second)
		}
	}
	result.DurationMs = time.Since(stepStarted).Milliseconds()
	if lastErr == nil {
		lastErr = fmt.Errorf("step execution failed")
	}
	return result, nil, lastErr
}

func (r *AgentWorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agscalev1alpha1.AgentWorkflowRun{}).
		Complete(r)
}

func stepTimeout(seconds int32) time.Duration {
	if seconds <= 0 {
		return 20 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func resolveStepEndpoints(baseURL string, step agscalev1alpha1.WorkflowStepSpec) []string {
	base := strings.TrimSuffix(baseURL, "/")
	if step.EndpointPath != "" {
		return []string{base + step.EndpointPath}
	}
	if step.Protocol == agscalev1alpha1.WorkflowProtocolMCP {
		return []string{base + "/mcp/invoke"}
	}
	// OpenClaw-compatible default: try invoke API then chat API.
	return []string{base + "/invoke", base + "/chat"}
}
