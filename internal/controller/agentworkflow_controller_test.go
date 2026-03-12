package controller

import (
	"strings"
	"testing"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

func TestValidateWorkflowSpecRejectsDependencyCycles(t *testing.T) {
	workflow := &agscalev1alpha1.AgentWorkflow{
		Spec: agscalev1alpha1.AgentWorkflowSpec{
			Steps: []agscalev1alpha1.WorkflowStepSpec{
				{
					Name:      "step-a",
					DependsOn: []string{"step-b"},
					Type:      agscalev1alpha1.WorkflowStepPrompt,
					Protocol:  agscalev1alpha1.WorkflowProtocolHTTP,
					AgentRef:  &agscalev1alpha1.LocalObjectReference{Name: "agent-a"},
				},
				{
					Name:      "step-b",
					DependsOn: []string{"step-a"},
					Type:      agscalev1alpha1.WorkflowStepPrompt,
					Protocol:  agscalev1alpha1.WorkflowProtocolHTTP,
					AgentRef:  &agscalev1alpha1.LocalObjectReference{Name: "agent-b"},
				},
			},
		},
	}

	err := validateWorkflowSpec(workflow)
	if err == nil {
		t.Fatalf("expected dependency cycle to be rejected")
	}
	if !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("expected dependency cycle error, got %v", err)
	}
}

func TestValidateWorkflowSpecAcceptsAcyclicWorkflow(t *testing.T) {
	workflow := &agscalev1alpha1.AgentWorkflow{
		Spec: agscalev1alpha1.AgentWorkflowSpec{
			Steps: []agscalev1alpha1.WorkflowStepSpec{
				{
					Name:     "step-a",
					Type:     agscalev1alpha1.WorkflowStepPrompt,
					Protocol: agscalev1alpha1.WorkflowProtocolHTTP,
					AgentRef: &agscalev1alpha1.LocalObjectReference{Name: "agent-a"},
				},
				{
					Name:      "step-b",
					DependsOn: []string{"step-a"},
					Type:      agscalev1alpha1.WorkflowStepPrompt,
					Protocol:  agscalev1alpha1.WorkflowProtocolHTTP,
					AgentRef:  &agscalev1alpha1.LocalObjectReference{Name: "agent-b"},
				},
			},
		},
	}

	if err := validateWorkflowSpec(workflow); err != nil {
		t.Fatalf("expected acyclic workflow to validate, got %v", err)
	}
}
