package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

const (
	usageRequestsAnn  = "agscale.io/usage-requests-24h"
	usageTokensAnn    = "agscale.io/usage-tokens-24h"
	usageCostAnn      = "agscale.io/usage-cost-usd-24h"
	usageUpdatedAtAnn = "agscale.io/usage-updated-at"
)

type AgentBudgetPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *AgentBudgetPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	started := time.Now()
	result := "success"
	defer func() {
		observeReconcile("AgentBudgetPolicy", result, started)
	}()

	policy := &agscalev1alpha1.AgentBudgetPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		result = "not_found"
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	agents, err := r.resolveTargetAgents(ctx, policy)
	if err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("AgentBudgetPolicy", "ResolveTargets").Inc()
		return ctrl.Result{}, err
	}

	usage, phase, reason := evaluateBudgetPolicyUsage(policy, agents)
	if policy.Status.Phase != phase {
		budgetEvaluationsTotal.WithLabelValues(string(phase), string(policy.Spec.Scope)).Inc()
	}
	if phase == agscalev1alpha1.BudgetPhaseExceeded {
		if exceededDimensions(policy.Spec.Limits, usage) > 0 {
			budgetExceededTotal.WithLabelValues(string(policy.Spec.Scope), "any").Inc()
		}
	}
	if phase == agscalev1alpha1.BudgetPhaseUnknown {
		budgetTelemetryStaleTotal.WithLabelValues("hybrid").Inc()
	}

	now := metav1.Now()
	windowStart := metav1.NewTime(now.Add(-24 * time.Hour))
	policy.Status.ObservedGeneration = policy.Generation
	policy.Status.LastEvaluatedTime = &now
	policy.Status.WindowStartTime = &windowStart
	policy.Status.EffectiveUsage = usage
	policy.Status.EffectiveLimits = policy.Spec.Limits
	policy.Status.Phase = phase
	policy.Status.Reason = reason
	setBudgetPolicyConditions(policy, phase, reason)

	if err := r.Status().Update(ctx, policy); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("AgentBudgetPolicy", "StatusUpdate").Inc()
		return ctrl.Result{}, err
	}

	for i := range agents {
		agent := &agents[i]
		if !policyAppliesToAgent(policy, agent) {
			continue
		}
		if err := r.projectBudgetStatusToAgent(ctx, policy, agent, usage, phase, reason); err != nil {
			if apierrors.IsConflict(err) {
				continue
			}
			logger.Error(err, "unable to project budget status to agent", "agent", agent.Name)
			reconcileErrorsTotal.WithLabelValues("AgentBudgetPolicy", "AgentStatusProject").Inc()
		}
	}

	interval := policy.Spec.Evaluation.IntervalSeconds
	if interval <= 0 {
		interval = 60
	}
	return ctrl.Result{RequeueAfter: time.Duration(interval) * time.Second}, nil
}

func (r *AgentBudgetPolicyReconciler) resolveTargetAgents(ctx context.Context, policy *agscalev1alpha1.AgentBudgetPolicy) ([]agscalev1alpha1.Agent, error) {
	switch policy.Spec.Scope {
	case agscalev1alpha1.BudgetScopeNamespace:
		targetNamespace := policy.Spec.TargetRef.Name
		if targetNamespace == "" {
			targetNamespace = policy.Namespace
		}
		var list agscalev1alpha1.AgentList
		if err := r.List(ctx, &list, client.InNamespace(targetNamespace)); err != nil {
			return nil, err
		}
		return list.Items, nil
	case agscalev1alpha1.BudgetScopeAgent:
		targetNamespace := policy.Spec.TargetRef.Namespace
		if targetNamespace == "" {
			targetNamespace = policy.Namespace
		}
		targetName := policy.Spec.TargetRef.Name
		agent := &agscalev1alpha1.Agent{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: targetNamespace, Name: targetName}, agent); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		return []agscalev1alpha1.Agent{*agent}, nil
	default:
		return nil, fmt.Errorf("unsupported budget scope %q", policy.Spec.Scope)
	}
}

func evaluateBudgetPolicyUsage(policy *agscalev1alpha1.AgentBudgetPolicy, agents []agscalev1alpha1.Agent) (agscalev1alpha1.BudgetUsageSnapshot, agscalev1alpha1.BudgetPhase, string) {
	usage := agscalev1alpha1.BudgetUsageSnapshot{}
	if len(agents) == 0 {
		return usage, agscalev1alpha1.BudgetPhaseUnknown, "NoTargets"
	}

	staleAfter := time.Duration(policy.Spec.Evaluation.StaleAfterSeconds) * time.Second
	if staleAfter <= 0 {
		staleAfter = 5 * time.Minute
	}

	telemetryAvailable := true
	for _, agent := range agents {
		u, ok := usageFromAgent(agent, staleAfter)
		if !ok {
			telemetryAvailable = false
			continue
		}
		usage.Requests24h += u.Requests24h
		usage.Tokens24h += u.Tokens24h
		usage.EstimatedCostUSD += u.EstimatedCostUSD
	}

	if !telemetryAvailable {
		return usage, agscalev1alpha1.BudgetPhaseUnknown, "TelemetryStale"
	}
	if exceedsBudget(policy.Spec.Limits, usage) {
		return usage, agscalev1alpha1.BudgetPhaseExceeded, "UsageExceeded"
	}
	return usage, agscalev1alpha1.BudgetPhaseWithinBudget, "WithinBudget"
}

func usageFromAgent(agent agscalev1alpha1.Agent, staleAfter time.Duration) (agscalev1alpha1.BudgetUsageSnapshot, bool) {
	anns := agent.GetAnnotations()
	if len(anns) == 0 {
		return agscalev1alpha1.BudgetUsageSnapshot{}, false
	}
	updatedAtRaw := anns[usageUpdatedAtAnn]
	if updatedAtRaw == "" {
		return agscalev1alpha1.BudgetUsageSnapshot{}, false
	}
	updatedAt, err := time.Parse(time.RFC3339, updatedAtRaw)
	if err != nil {
		return agscalev1alpha1.BudgetUsageSnapshot{}, false
	}
	if time.Since(updatedAt) > staleAfter {
		return agscalev1alpha1.BudgetUsageSnapshot{}, false
	}

	requests, err := parseInt64Annotation(anns, usageRequestsAnn)
	if err != nil {
		return agscalev1alpha1.BudgetUsageSnapshot{}, false
	}
	tokens, err := parseInt64Annotation(anns, usageTokensAnn)
	if err != nil {
		return agscalev1alpha1.BudgetUsageSnapshot{}, false
	}
	cost, err := parseFloat64Annotation(anns, usageCostAnn)
	if err != nil {
		return agscalev1alpha1.BudgetUsageSnapshot{}, false
	}

	return agscalev1alpha1.BudgetUsageSnapshot{
		Requests24h:      requests,
		Tokens24h:        tokens,
		EstimatedCostUSD: cost,
	}, true
}

func parseInt64Annotation(anns map[string]string, key string) (int64, error) {
	v := anns[key]
	if v == "" {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

func parseFloat64Annotation(anns map[string]string, key string) (float64, error) {
	v := anns[key]
	if v == "" {
		return 0, nil
	}
	return strconv.ParseFloat(v, 64)
}

func exceedsBudget(limit agscalev1alpha1.BudgetLimitSpec, usage agscalev1alpha1.BudgetUsageSnapshot) bool {
	if limit.MaxRequests24h != nil && usage.Requests24h > *limit.MaxRequests24h {
		return true
	}
	if limit.MaxTokens24h != nil && usage.Tokens24h > *limit.MaxTokens24h {
		return true
	}
	if limit.MaxEstimatedCostUSD24h != nil && usage.EstimatedCostUSD > *limit.MaxEstimatedCostUSD24h {
		return true
	}
	return false
}

func exceededDimensions(limit agscalev1alpha1.BudgetLimitSpec, usage agscalev1alpha1.BudgetUsageSnapshot) int {
	count := 0
	if limit.MaxRequests24h != nil && usage.Requests24h > *limit.MaxRequests24h {
		count++
	}
	if limit.MaxTokens24h != nil && usage.Tokens24h > *limit.MaxTokens24h {
		count++
	}
	if limit.MaxEstimatedCostUSD24h != nil && usage.EstimatedCostUSD > *limit.MaxEstimatedCostUSD24h {
		count++
	}
	return count
}

func setBudgetPolicyConditions(policy *agscalev1alpha1.AgentBudgetPolicy, phase agscalev1alpha1.BudgetPhase, reason string) {
	now := metav1.Now()
	telemetryStatus := metav1.ConditionTrue
	if phase == agscalev1alpha1.BudgetPhaseUnknown {
		telemetryStatus = metav1.ConditionFalse
	}
	policy.Status.Conditions = setCondition(policy.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Evaluated",
		Message:            "budget policy evaluated",
		LastTransitionTime: now,
		ObservedGeneration: policy.Generation,
	})
	policy.Status.Conditions = setCondition(policy.Status.Conditions, metav1.Condition{
		Type:               "TelemetryAvailable",
		Status:             telemetryStatus,
		Reason:             reason,
		Message:            reason,
		LastTransitionTime: now,
		ObservedGeneration: policy.Generation,
	})
	budgetStatus := metav1.ConditionFalse
	if phase == agscalev1alpha1.BudgetPhaseExceeded {
		budgetStatus = metav1.ConditionTrue
	} else if phase == agscalev1alpha1.BudgetPhaseUnknown {
		budgetStatus = metav1.ConditionUnknown
	}
	policy.Status.Conditions = setCondition(policy.Status.Conditions, metav1.Condition{
		Type:               "BudgetExceeded",
		Status:             budgetStatus,
		Reason:             reason,
		Message:            reason,
		LastTransitionTime: now,
		ObservedGeneration: policy.Generation,
	})
}

func setCondition(conditions []metav1.Condition, condition metav1.Condition) []metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condition.Type {
			condition.LastTransitionTime = conditions[i].LastTransitionTime
			if conditions[i].Status != condition.Status || conditions[i].Reason != condition.Reason || conditions[i].Message != condition.Message {
				condition.LastTransitionTime = metav1.Now()
			}
			conditions[i] = condition
			return conditions
		}
	}
	return append(conditions, condition)
}

func policyAppliesToAgent(policy *agscalev1alpha1.AgentBudgetPolicy, agent *agscalev1alpha1.Agent) bool {
	if policy.Spec.Scope == agscalev1alpha1.BudgetScopeAgent {
		targetNamespace := policy.Spec.TargetRef.Namespace
		if targetNamespace == "" {
			targetNamespace = policy.Namespace
		}
		return agent.Namespace == targetNamespace && agent.Name == policy.Spec.TargetRef.Name
	}
	if policy.Spec.Scope != agscalev1alpha1.BudgetScopeNamespace {
		return false
	}
	targetNamespace := policy.Spec.TargetRef.Name
	if targetNamespace == "" {
		targetNamespace = policy.Namespace
	}
	if agent.Namespace != targetNamespace {
		return false
	}
	if agent.Spec.BudgetPolicyRef != nil && agent.Spec.BudgetPolicyRef.Name != "" {
		return false
	}
	return true
}

func (r *AgentBudgetPolicyReconciler) projectBudgetStatusToAgent(ctx context.Context, policy *agscalev1alpha1.AgentBudgetPolicy, sourceAgent *agscalev1alpha1.Agent, usage agscalev1alpha1.BudgetUsageSnapshot, phase agscalev1alpha1.BudgetPhase, reason string) error {
	agent := &agscalev1alpha1.Agent{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: sourceAgent.Namespace, Name: sourceAgent.Name}, agent); err != nil {
		return err
	}
	oldPhase := agent.Status.BudgetPhase
	now := metav1.Now()
	agent.Status.BudgetPhase = string(phase)
	agent.Status.BudgetReason = reason
	agent.Status.BudgetLastEvaluatedTime = &now
	agent.Status.BudgetUsageSnapshot = usage
	status := metav1.ConditionFalse
	if phase == agscalev1alpha1.BudgetPhaseExceeded {
		status = metav1.ConditionTrue
	} else if phase == agscalev1alpha1.BudgetPhaseUnknown {
		status = metav1.ConditionUnknown
	}
	agent.Status.Conditions = setCondition(agent.Status.Conditions, metav1.Condition{
		Type:               "BudgetExceeded",
		Status:             status,
		Reason:             reason,
		Message:            fmt.Sprintf("policy=%s phase=%s", policy.Name, phase),
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: agent.Generation,
	})
	if err := r.Status().Update(ctx, agent); err != nil {
		return err
	}
	if r.Recorder != nil && oldPhase != string(phase) {
		eventType := corev1.EventTypeNormal
		if phase == agscalev1alpha1.BudgetPhaseExceeded || phase == agscalev1alpha1.BudgetPhaseUnknown {
			eventType = corev1.EventTypeWarning
		}
		r.Recorder.Eventf(agent, eventType, "BudgetPhaseChanged", "policy=%s phase=%s reason=%s", policy.Name, phase, reason)
	}
	return nil
}

func (r *AgentBudgetPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agscalev1alpha1.AgentBudgetPolicy{}).
		Complete(r)
}
