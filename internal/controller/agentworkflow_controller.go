package controller

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

type AgentWorkflowReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *AgentWorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	started := time.Now()
	result := "success"
	defer func() {
		observeReconcile("AgentWorkflow", result, started)
	}()

	workflow := &agscalev1alpha1.AgentWorkflow{}
	if err := r.Get(ctx, req.NamespacedName, workflow); err != nil {
		result = "not_found"
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	now := metav1.Now()
	workflow.Status.ObservedGeneration = workflow.Generation
	workflow.Status.Phase = "Ready"
	if err := validateWorkflowSpec(workflow); err != nil {
		workflow.Status.Phase = "Invalid"
		setConditionWorkflow(&workflow.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "InvalidSpec",
			Message:            err.Error(),
			LastTransitionTime: now,
			ObservedGeneration: workflow.Generation,
		})
		if err := r.Status().Update(ctx, workflow); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("AgentWorkflow", "StatusUpdate").Inc()
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	setConditionWorkflow(&workflow.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Validated",
		Message:            "workflow is accepted",
		LastTransitionTime: now,
		ObservedGeneration: workflow.Generation,
	})
	if err := r.Status().Update(ctx, workflow); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("AgentWorkflow", "StatusUpdate").Inc()
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *AgentWorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agscalev1alpha1.AgentWorkflow{}).
		Complete(r)
}

func setConditionWorkflow(conditions *[]metav1.Condition, condition metav1.Condition) {
	for i := range *conditions {
		if (*conditions)[i].Type == condition.Type {
			(*conditions)[i] = condition
			return
		}
	}
	*conditions = append(*conditions, condition)
}

func validateWorkflowSpec(workflow *agscalev1alpha1.AgentWorkflow) error {
	if len(workflow.Spec.Steps) == 0 {
		return fmt.Errorf("spec.steps must not be empty")
	}
	seen := map[string]bool{}
	for i, step := range workflow.Spec.Steps {
		if step.Name == "" {
			return fmt.Errorf("spec.steps[%d].name is required", i)
		}
		if seen[step.Name] {
			return fmt.Errorf("duplicate step name %q", step.Name)
		}
		seen[step.Name] = true
		if step.Protocol != "" &&
			step.Protocol != agscalev1alpha1.WorkflowProtocolHTTP &&
			step.Protocol != agscalev1alpha1.WorkflowProtocolMCP &&
			step.Protocol != agscalev1alpha1.WorkflowProtocolA2A {
			return fmt.Errorf("spec.steps[%d].protocol %q is unsupported", i, step.Protocol)
		}
		if step.Protocol == agscalev1alpha1.WorkflowProtocolA2A {
			if (step.AgentRef == nil || step.AgentRef.Name == "") && (step.IntegrationRef == nil || step.IntegrationRef.Name == "") {
				return fmt.Errorf("spec.steps[%d] protocol a2a requires agentRef or integrationRef", i)
			}
		} else {
			if step.AgentRef == nil || step.AgentRef.Name == "" {
				return fmt.Errorf("spec.steps[%d].agentRef.name is required for protocol %q", i, step.Protocol)
			}
		}
	}
	for i, step := range workflow.Spec.Steps {
		for _, dep := range step.DependsOn {
			if !seen[dep] {
				return fmt.Errorf("spec.steps[%d].dependsOn references unknown step %q", i, dep)
			}
		}
	}
	return nil
}
