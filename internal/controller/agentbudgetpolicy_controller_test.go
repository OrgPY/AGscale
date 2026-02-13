package controller

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

func TestEvaluateBudgetPolicyUsageExceeded(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	requestsLimit := int64(100)
	policy := &agscalev1alpha1.AgentBudgetPolicy{
		Spec: agscalev1alpha1.AgentBudgetPolicySpec{
			Scope: agscalev1alpha1.BudgetScopeAgent,
			Limits: agscalev1alpha1.BudgetLimitSpec{
				MaxRequests24h: &requestsLimit,
			},
			Evaluation: agscalev1alpha1.BudgetEvaluationSpec{StaleAfterSeconds: 300},
		},
	}
	agents := []agscalev1alpha1.Agent{{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			usageUpdatedAtAnn: now,
			usageRequestsAnn:  "101",
		}},
	}}

	usage, phase, reason := evaluateBudgetPolicyUsage(policy, agents)
	if phase != agscalev1alpha1.BudgetPhaseExceeded {
		t.Fatalf("expected phase Exceeded, got %s", phase)
	}
	if reason != "UsageExceeded" {
		t.Fatalf("expected reason UsageExceeded, got %s", reason)
	}
	if usage.Requests24h != 101 {
		t.Fatalf("expected requests=101, got %d", usage.Requests24h)
	}
}

func TestEvaluateBudgetPolicyUsageUnknownOnStale(t *testing.T) {
	old := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	tokensLimit := int64(1000)
	policy := &agscalev1alpha1.AgentBudgetPolicy{
		Spec: agscalev1alpha1.AgentBudgetPolicySpec{
			Scope: agscalev1alpha1.BudgetScopeAgent,
			Limits: agscalev1alpha1.BudgetLimitSpec{
				MaxTokens24h: &tokensLimit,
			},
			Evaluation: agscalev1alpha1.BudgetEvaluationSpec{StaleAfterSeconds: 60},
		},
	}
	agents := []agscalev1alpha1.Agent{{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			usageUpdatedAtAnn: old,
			usageTokensAnn:    "2000",
		}},
	}}

	_, phase, reason := evaluateBudgetPolicyUsage(policy, agents)
	if phase != agscalev1alpha1.BudgetPhaseUnknown {
		t.Fatalf("expected phase Unknown, got %s", phase)
	}
	if reason != "TelemetryStale" {
		t.Fatalf("expected reason TelemetryStale, got %s", reason)
	}
}

func TestPolicyAppliesToAgent(t *testing.T) {
	policy := &agscalev1alpha1.AgentBudgetPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "ns-budget", Namespace: "team-a"},
		Spec: agscalev1alpha1.AgentBudgetPolicySpec{
			Scope: agscalev1alpha1.BudgetScopeNamespace,
			TargetRef: agscalev1alpha1.BudgetTargetRef{
				Name: "team-a",
			},
		},
	}
	agent := &agscalev1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: "a1", Namespace: "team-a"}}
	if !policyAppliesToAgent(policy, agent) {
		t.Fatal("expected namespace policy to apply")
	}
	agent.Spec.BudgetPolicyRef = &agscalev1alpha1.LocalObjectReference{Name: "override"}
	if policyAppliesToAgent(policy, agent) {
		t.Fatal("expected namespace policy not to apply when agent has explicit ref")
	}

	agentPolicy := &agscalev1alpha1.AgentBudgetPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "a1-budget", Namespace: "team-a"},
		Spec: agscalev1alpha1.AgentBudgetPolicySpec{
			Scope: agscalev1alpha1.BudgetScopeAgent,
			TargetRef: agscalev1alpha1.BudgetTargetRef{
				Name: "a1",
			},
		},
	}
	agent.Spec.BudgetPolicyRef = nil
	if !policyAppliesToAgent(agentPolicy, agent) {
		t.Fatal("expected agent policy to apply to target agent")
	}
	agent.Name = "a2"
	if policyAppliesToAgent(agentPolicy, agent) {
		t.Fatal(fmt.Sprintf("expected agent policy not to apply to non-target, got name=%s", agent.Name))
	}
}
