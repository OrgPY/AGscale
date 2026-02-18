package controller

import (
	"context"
	"testing"

	agscalev1alpha1 "agscale/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEventSourceReconcileCreatesWorkflowRun(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := agscalev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	workflow := &agscalev1alpha1.AgentWorkflow{
		ObjectMeta: metav1.ObjectMeta{Name: "wf-a", Namespace: "default"},
		Spec: agscalev1alpha1.AgentWorkflowSpec{
			Mode: agscalev1alpha1.WorkflowModeRequestResponse,
			Steps: []agscalev1alpha1.WorkflowStepSpec{{
				Name:     "step-a",
				Type:     agscalev1alpha1.WorkflowStepPrompt,
				Protocol: agscalev1alpha1.WorkflowProtocolHTTP,
				AgentRef: &agscalev1alpha1.LocalObjectReference{Name: "agent-a"},
			}},
		},
	}
	source := &agscalev1alpha1.EventSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-a",
			Namespace: "default",
			Annotations: map[string]string{
				"agscale.io/interval-seconds": "60",
			},
		},
		Spec: agscalev1alpha1.EventSourceSpec{
			TargetWorkflowRef: agscalev1alpha1.LocalObjectReference{Name: workflow.Name},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(source).
		WithObjects(workflow, source).
		Build()

	reconciler := &EventSourceReconciler{Client: c, Scheme: scheme}
	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: source.Name, Namespace: source.Namespace}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	runList := &agscalev1alpha1.AgentWorkflowRunList{}
	if err := c.List(ctx, runList); err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runList.Items) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runList.Items))
	}
	if runList.Items[0].Spec.WorkflowRef.Name != workflow.Name {
		t.Fatalf("expected run workflowRef=%s, got %s", workflow.Name, runList.Items[0].Spec.WorkflowRef.Name)
	}

	updatedSource := &agscalev1alpha1.EventSource{}
	if err := c.Get(ctx, types.NamespacedName{Name: source.Name, Namespace: source.Namespace}, updatedSource); err != nil {
		t.Fatalf("get source: %v", err)
	}
	if updatedSource.Status.Phase != "Active" {
		t.Fatalf("expected phase Active, got %s", updatedSource.Status.Phase)
	}
	if updatedSource.Status.LastEventTime == nil {
		t.Fatalf("expected LastEventTime to be set")
	}
}
