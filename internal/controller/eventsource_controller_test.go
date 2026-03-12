package controller

import (
	"context"
	"strings"
	"testing"
	"time"

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
	integration := &agscalev1alpha1.IntegrationEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "integration-a", Namespace: "default"},
		Status: agscalev1alpha1.IntegrationEndpointStatus{
			Ready: true,
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
			IntegrationRef:    agscalev1alpha1.LocalObjectReference{Name: integration.Name},
			Topic:             "orders.created",
			Filter:            "severity=high",
			Delivery:          agscalev1alpha1.EventSourceDelivery{Mode: "Polling"},
			TargetWorkflowRef: agscalev1alpha1.LocalObjectReference{Name: workflow.Name},
		},
	}
	triggerTime := time.Unix(1712345678, 123456789)

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(source).
		WithObjects(workflow, integration, source).
		Build()

	reconciler := &EventSourceReconciler{
		Client: c,
		Scheme: scheme,
		Now:    func() time.Time { return triggerTime },
	}
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
	run := runList.Items[0]
	if run.Spec.WorkflowRef.Name != workflow.Name {
		t.Fatalf("expected run workflowRef=%s, got %s", workflow.Name, run.Spec.WorkflowRef.Name)
	}
	if run.Spec.Trigger != "eventsource:source-a:orders.created" {
		t.Fatalf("unexpected trigger %q", run.Spec.Trigger)
	}
	if got := run.Annotations["agscale.io/event-topic"]; got != source.Spec.Topic {
		t.Fatalf("expected topic annotation %q, got %q", source.Spec.Topic, got)
	}
	if got := run.Annotations["agscale.io/event-filter"]; got != source.Spec.Filter {
		t.Fatalf("expected filter annotation %q, got %q", source.Spec.Filter, got)
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
	if updatedSource.Status.LastEventTime.Unix() != triggerTime.Unix() {
		t.Fatalf("expected LastEventTime unix %d, got %d", triggerTime.Unix(), updatedSource.Status.LastEventTime.Unix())
	}
}

func TestEventSourceReconcileRequiresReadyIntegration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := agscalev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	workflow := newEventSourceTestWorkflow()
	integration := &agscalev1alpha1.IntegrationEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "integration-a", Namespace: "default"},
		Status: agscalev1alpha1.IntegrationEndpointStatus{
			Ready:     false,
			LastError: "connection refused",
		},
	}
	source := newEventSourceTestSource(workflow.Name, integration.Name)

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(source).
		WithObjects(workflow, integration, source).
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
	if len(runList.Items) != 0 {
		t.Fatalf("expected no workflow runs when integration is not ready, got %d", len(runList.Items))
	}

	updatedSource := &agscalev1alpha1.EventSource{}
	if err := c.Get(ctx, types.NamespacedName{Name: source.Name, Namespace: source.Namespace}, updatedSource); err != nil {
		t.Fatalf("get source: %v", err)
	}
	if updatedSource.Status.Phase != "Degraded" {
		t.Fatalf("expected phase Degraded, got %s", updatedSource.Status.Phase)
	}
	cond := findCondition(updatedSource.Status.Conditions, "Ready")
	if cond == nil || cond.Reason != "IntegrationNotReady" || !strings.Contains(cond.Message, integration.Status.LastError) {
		t.Fatalf("expected IntegrationNotReady condition, got %+v", cond)
	}
}

func TestEventSourceReconcileRejectsUnsupportedDeliveryMode(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := agscalev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	source := newEventSourceTestSource("wf-a", "integration-a")
	source.Spec.Delivery.Mode = "Push"

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(source).
		WithObjects(source).
		Build()

	reconciler := &EventSourceReconciler{Client: c, Scheme: scheme}
	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: source.Name, Namespace: source.Namespace}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	updatedSource := &agscalev1alpha1.EventSource{}
	if err := c.Get(ctx, types.NamespacedName{Name: source.Name, Namespace: source.Namespace}, updatedSource); err != nil {
		t.Fatalf("get source: %v", err)
	}
	if updatedSource.Status.Phase != "Degraded" {
		t.Fatalf("expected phase Degraded, got %s", updatedSource.Status.Phase)
	}
	cond := findCondition(updatedSource.Status.Conditions, "Ready")
	if cond == nil || cond.Reason != "InvalidSpec" {
		t.Fatalf("expected InvalidSpec condition, got %+v", cond)
	}
}

func TestEventSourceReconcileAlreadyExistsIsNonFatal(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := agscalev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	workflow := newEventSourceTestWorkflow()
	integration := &agscalev1alpha1.IntegrationEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "integration-a", Namespace: "default"},
		Status: agscalev1alpha1.IntegrationEndpointStatus{
			Ready: true,
		},
	}
	source := newEventSourceTestSource(workflow.Name, integration.Name)
	triggerTime := time.Unix(1712345678, 123456789)
	existingRun := &agscalev1alpha1.AgentWorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-a-trigger-1712345678123456789",
			Namespace: "default",
		},
		Spec: agscalev1alpha1.AgentWorkflowRunSpec{
			WorkflowRef: agscalev1alpha1.LocalObjectReference{Name: workflow.Name},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(source).
		WithObjects(workflow, integration, source, existingRun).
		Build()

	reconciler := &EventSourceReconciler{
		Client: c,
		Scheme: scheme,
		Now:    func() time.Time { return triggerTime },
	}
	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: source.Name, Namespace: source.Namespace}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	updatedSource := &agscalev1alpha1.EventSource{}
	if err := c.Get(ctx, types.NamespacedName{Name: source.Name, Namespace: source.Namespace}, updatedSource); err != nil {
		t.Fatalf("get source: %v", err)
	}
	if updatedSource.Status.Phase != "Active" {
		t.Fatalf("expected phase Active, got %s", updatedSource.Status.Phase)
	}
	cond := findCondition(updatedSource.Status.Conditions, "Ready")
	if cond == nil || cond.Reason != "DuplicateTriggerIgnored" {
		t.Fatalf("expected DuplicateTriggerIgnored condition, got %+v", cond)
	}
}

func newEventSourceTestWorkflow() *agscalev1alpha1.AgentWorkflow {
	return &agscalev1alpha1.AgentWorkflow{
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
}

func newEventSourceTestSource(workflowName, integrationName string) *agscalev1alpha1.EventSource {
	return &agscalev1alpha1.EventSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-a",
			Namespace: "default",
			Annotations: map[string]string{
				"agscale.io/interval-seconds": "60",
			},
		},
		Spec: agscalev1alpha1.EventSourceSpec{
			IntegrationRef:    agscalev1alpha1.LocalObjectReference{Name: integrationName},
			Delivery:          agscalev1alpha1.EventSourceDelivery{Mode: "Polling"},
			TargetWorkflowRef: agscalev1alpha1.LocalObjectReference{Name: workflowName},
		},
	}
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
