package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

func TestApplyPolicyDefaults(t *testing.T) {
	p := &agscalev1alpha1.AgentReliabilityPolicy{}
	applyPolicyDefaults(p)

	if p.Spec.Health.MinReadyPercent != 80 {
		t.Fatalf("expected default minReadyPercent=80, got %d", p.Spec.Health.MinReadyPercent)
	}
	if p.Spec.Remediation.MaxActionsPerHour != 6 {
		t.Fatalf("expected default maxActionsPerHour=6, got %d", p.Spec.Remediation.MaxActionsPerHour)
	}
	if len(p.Spec.Remediation.Actions) == 0 {
		t.Fatalf("expected default remediation actions")
	}
}

func TestCanRemediateRespectsCooldownAndLimit(t *testing.T) {
	now := time.Now()
	policy := &agscalev1alpha1.AgentReliabilityPolicy{
		Spec: agscalev1alpha1.AgentReliabilityPolicySpec{
			Remediation: agscalev1alpha1.ReliabilityRemediationSpec{
				MaxActionsPerHour: 2,
				CooldownSeconds:   300,
			},
		},
	}
	agent := &agscalev1alpha1.Agent{
		Status: agscalev1alpha1.AgentStatus{
			LastRemediationTime: &metav1.Time{Time: now.Add(-2 * time.Minute)},
			RecentRemediations: []agscalev1alpha1.RemediationRecord{
				{Action: "RestartPods", Timestamp: metav1.Time{Time: now.Add(-30 * time.Minute)}},
			},
		},
	}

	if canRemediate(agent, policy, now) {
		t.Fatalf("expected cooldown to block remediation")
	}

	agent.Status.LastRemediationTime = &metav1.Time{Time: now.Add(-10 * time.Minute)}
	agent.Status.RecentRemediations = append(agent.Status.RecentRemediations, agscalev1alpha1.RemediationRecord{
		Action:    "ScaleOut",
		Timestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
	})
	if canRemediate(agent, policy, now) {
		t.Fatalf("expected action-per-hour limit to block remediation")
	}

	agent.Status.RecentRemediations = []agscalev1alpha1.RemediationRecord{
		{Action: "RestartPods", Timestamp: metav1.Time{Time: now.Add(-70 * time.Minute)}},
	}
	if !canRemediate(agent, policy, now) {
		t.Fatalf("expected remediation to be allowed")
	}
}
