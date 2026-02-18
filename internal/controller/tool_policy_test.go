package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

func TestToolPolicyAppliesToAgent(t *testing.T) {
	agent := &agscalev1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Labels: map[string]string{"team": "ml"}}}
	policy := &agscalev1alpha1.AgentToolPolicy{Spec: agscalev1alpha1.AgentToolPolicySpec{AgentSelector: agscalev1alpha1.AgentSelectorSpec{Names: []string{"agent-a"}}}}
	if !toolPolicyAppliesToAgent(policy, agent) {
		t.Fatalf("expected policy to match by name")
	}
	policy.Spec.AgentSelector = agscalev1alpha1.AgentSelectorSpec{MatchLabels: map[string]string{"team": "ml"}}
	if !toolPolicyAppliesToAgent(policy, agent) {
		t.Fatalf("expected policy to match by label")
	}
}

func TestEnforceToolPolicyOnSkill(t *testing.T) {
	agent := &agscalev1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: "agent-a"}}
	policy := &agscalev1alpha1.AgentToolPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
		Spec: agscalev1alpha1.AgentToolPolicySpec{
			ApprovalMode:     "Auto",
			AllowedSkillRefs: []agscalev1alpha1.LocalObjectReference{{Name: "allowed-skill"}},
			AllowedProtocols: []string{"openapi-http"},
		},
	}
	skillAllowed := &agscalev1alpha1.AgentSkill{
		ObjectMeta: metav1.ObjectMeta{Name: "allowed-skill"},
		Spec:       agscalev1alpha1.AgentSkillSpec{Source: &agscalev1alpha1.AgentSkillSourceSpec{Protocol: "openapi-http", URL: "https://example.com"}},
	}
	if err := enforceToolPolicyOnSkill(agent, policy, skillAllowed); err != nil {
		t.Fatalf("expected allowed skill, got error: %v", err)
	}
	skillDenied := &agscalev1alpha1.AgentSkill{ObjectMeta: metav1.ObjectMeta{Name: "other-skill"}}
	if err := enforceToolPolicyOnSkill(agent, policy, skillDenied); err == nil {
		t.Fatalf("expected denied skill by ref allowlist")
	}
}
