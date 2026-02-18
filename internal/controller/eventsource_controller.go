package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	agscalev1alpha1 "agscale/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type EventSourceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *EventSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	started := time.Now()
	result := "success"
	defer func() { observeReconcile("EventSource", result, started) }()

	source := &agscalev1alpha1.EventSource{}
	if err := r.Get(ctx, req.NamespacedName, source); err != nil {
		result = "not_found"
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	interval := eventSourceIntervalSeconds(source)
	now := metav1.Now()
	source.Status.ObservedGeneration = source.Generation

	if source.Spec.TargetWorkflowRef.Name == "" {
		source.Status.Phase = "Degraded"
		setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "MissingTargetWorkflow", Message: "targetWorkflowRef.name is required", LastTransitionTime: now, ObservedGeneration: source.Generation})
		if err := r.Status().Update(ctx, source); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("EventSource", "StatusUpdate").Inc()
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Duration(interval) * time.Second}, nil
	}

	workflow := &agscalev1alpha1.AgentWorkflow{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: source.Namespace, Name: source.Spec.TargetWorkflowRef.Name}, workflow); err != nil {
		source.Status.Phase = "Degraded"
		setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "TargetWorkflowNotFound", Message: err.Error(), LastTransitionTime: now, ObservedGeneration: source.Generation})
		if err := r.Status().Update(ctx, source); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("EventSource", "StatusUpdate").Inc()
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Duration(interval) * time.Second}, nil
	}

	shouldTrigger := false
	if source.Status.LastEventTime == nil {
		shouldTrigger = true
	} else if time.Since(source.Status.LastEventTime.Time) >= time.Duration(interval)*time.Second {
		shouldTrigger = true
	}

	if shouldTrigger {
		run := &agscalev1alpha1.AgentWorkflowRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-trigger-%d", source.Name, time.Now().Unix()),
				Namespace: source.Namespace,
			},
			Spec: agscalev1alpha1.AgentWorkflowRunSpec{
				WorkflowRef:   agscalev1alpha1.LocalObjectReference{Name: source.Spec.TargetWorkflowRef.Name},
				Trigger:       "eventsource:" + source.Name,
				CorrelationID: fmt.Sprintf("%s-%d", source.Name, time.Now().Unix()),
			},
		}
		if err := r.Create(ctx, run); err == nil {
			t := metav1.Now()
			source.Status.LastEventTime = &t
			source.Status.Lag = 0
			source.Status.Phase = "Active"
			setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Triggered", Message: "workflow run created", LastTransitionTime: now, ObservedGeneration: source.Generation})
			eventSourceTriggersTotal.WithLabelValues(source.Name).Inc()
		} else {
			source.Status.Phase = "Degraded"
			setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "TriggerCreateFailed", Message: err.Error(), LastTransitionTime: now, ObservedGeneration: source.Generation})
		}
	} else {
		source.Status.Phase = "Active"
		setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Watching", Message: "event source active", LastTransitionTime: now, ObservedGeneration: source.Generation})
	}

	if err := r.Status().Update(ctx, source); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("EventSource", "StatusUpdate").Inc()
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: time.Duration(interval) * time.Second}, nil
}

func (r *EventSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agscalev1alpha1.EventSource{}).
		Complete(r)
}

func eventSourceIntervalSeconds(source *agscalev1alpha1.EventSource) int {
	if source == nil {
		return 300
	}
	if source.Annotations != nil {
		if raw := source.Annotations["agscale.io/interval-seconds"]; raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				return v
			}
		}
	}
	return 300
}
