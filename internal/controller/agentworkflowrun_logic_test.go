package controller

import (
	"testing"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

func TestAllowedWorkflowDestinationUsesIntegrationRef(t *testing.T) {
	t.Parallel()

	step := agscalev1alpha1.WorkflowStepSpec{
		IntegrationRef: &agscalev1alpha1.LocalObjectReference{Name: "remote-a2a-hub"},
		Protocol:       agscalev1alpha1.WorkflowProtocolA2A,
	}

	if !isAllowedWorkflowDestination([]string{"remote-a2a-hub"}, step) {
		t.Fatalf("expected integrationRef target to satisfy allowed destination policy")
	}
	if isAllowedWorkflowDestination([]string{"different-target"}, step) {
		t.Fatalf("expected integrationRef target mismatch to be denied")
	}
}
