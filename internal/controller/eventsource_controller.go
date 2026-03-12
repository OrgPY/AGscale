package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	agscalev1alpha1 "agscale/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	Now      func() time.Time
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
	triggerTime := r.now()
	now := metav1.NewTime(triggerTime)
	source.Status.ObservedGeneration = source.Generation

	if err := validateEventSourceSpec(source); err != nil {
		source.Status.Phase = "Degraded"
		setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "InvalidSpec", Message: err.Error(), LastTransitionTime: now, ObservedGeneration: source.Generation})
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

	integration := &agscalev1alpha1.IntegrationEndpoint{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: source.Namespace, Name: source.Spec.IntegrationRef.Name}, integration); err != nil {
		source.Status.Phase = "Degraded"
		setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "IntegrationNotFound", Message: err.Error(), LastTransitionTime: now, ObservedGeneration: source.Generation})
		if err := r.Status().Update(ctx, source); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("EventSource", "StatusUpdate").Inc()
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Duration(interval) * time.Second}, nil
	}
	if !integration.Status.Ready {
		source.Status.Phase = "Degraded"
		message := "integration endpoint is not ready"
		if integration.Status.LastError != "" {
			message = integration.Status.LastError
		}
		setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "IntegrationNotReady", Message: message, LastTransitionTime: now, ObservedGeneration: source.Generation})
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
	} else if triggerTime.Sub(source.Status.LastEventTime.Time) >= time.Duration(interval)*time.Second {
		shouldTrigger = true
	}

	if shouldTrigger {
		run := &agscalev1alpha1.AgentWorkflowRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("%s-trigger-%d", source.Name, triggerTime.UnixNano()),
				Namespace:   source.Namespace,
				Annotations: eventSourceRunAnnotations(source),
			},
			Spec: agscalev1alpha1.AgentWorkflowRunSpec{
				WorkflowRef:   agscalev1alpha1.LocalObjectReference{Name: source.Spec.TargetWorkflowRef.Name},
				Trigger:       eventSourceTrigger(source),
				CorrelationID: fmt.Sprintf("%s-%d", source.Name, triggerTime.UnixNano()),
			},
		}
		if err := r.Create(ctx, run); err == nil {
			t := metav1.NewTime(triggerTime)
			source.Status.LastEventTime = &t
			source.Status.Lag = 0
			source.Status.Phase = "Active"
			setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Triggered", Message: eventSourceWatchingMessage(source), LastTransitionTime: now, ObservedGeneration: source.Generation})
			eventSourceTriggersTotal.WithLabelValues(source.Name).Inc()
		} else if apierrors.IsAlreadyExists(err) {
			t := metav1.NewTime(triggerTime)
			source.Status.LastEventTime = &t
			source.Status.Lag = 0
			source.Status.Phase = "Active"
			setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "DuplicateTriggerIgnored", Message: "workflow run for current polling tick already exists", LastTransitionTime: now, ObservedGeneration: source.Generation})
		} else {
			source.Status.Phase = "Degraded"
			setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "TriggerCreateFailed", Message: err.Error(), LastTransitionTime: now, ObservedGeneration: source.Generation})
		}
	} else {
		source.Status.Phase = "Active"
		setConditionWorkflow(&source.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Watching", Message: eventSourceWatchingMessage(source), LastTransitionTime: now, ObservedGeneration: source.Generation})
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

func (r *EventSourceReconciler) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func validateEventSourceSpec(source *agscalev1alpha1.EventSource) error {
	if source == nil {
		return errors.New("event source is required")
	}
	if source.Spec.TargetWorkflowRef.Name == "" {
		return errors.New("targetWorkflowRef.name is required")
	}
	if source.Spec.IntegrationRef.Name == "" {
		return errors.New("integrationRef.name is required")
	}
	mode := eventSourceDeliveryMode(source)
	if mode != "Polling" {
		return fmt.Errorf("delivery.mode %q is unsupported; supported: Polling", source.Spec.Delivery.Mode)
	}
	return nil
}

func eventSourceDeliveryMode(source *agscalev1alpha1.EventSource) string {
	if source == nil {
		return "Polling"
	}
	mode := strings.TrimSpace(source.Spec.Delivery.Mode)
	if mode == "" {
		return "Polling"
	}
	if strings.EqualFold(mode, "polling") {
		return "Polling"
	}
	return mode
}

func eventSourceTrigger(source *agscalev1alpha1.EventSource) string {
	if source == nil {
		return "eventsource"
	}
	trigger := "eventsource:" + source.Name
	if topic := strings.TrimSpace(source.Spec.Topic); topic != "" {
		trigger += ":" + topic
	}
	return trigger
}

func eventSourceRunAnnotations(source *agscalev1alpha1.EventSource) map[string]string {
	if source == nil {
		return nil
	}
	annotations := map[string]string{
		"agscale.io/event-source":        source.Name,
		"agscale.io/event-integration":   source.Spec.IntegrationRef.Name,
		"agscale.io/event-delivery-mode": eventSourceDeliveryMode(source),
	}
	if topic := strings.TrimSpace(source.Spec.Topic); topic != "" {
		annotations["agscale.io/event-topic"] = topic
	}
	if filter := strings.TrimSpace(source.Spec.Filter); filter != "" {
		annotations["agscale.io/event-filter"] = filter
	}
	return annotations
}

func eventSourceWatchingMessage(source *agscalev1alpha1.EventSource) string {
	if source == nil {
		return "event source active"
	}
	message := fmt.Sprintf("polling integration %q", source.Spec.IntegrationRef.Name)
	if topic := strings.TrimSpace(source.Spec.Topic); topic != "" {
		message += fmt.Sprintf(" topic %q", topic)
	}
	if filter := strings.TrimSpace(source.Spec.Filter); filter != "" {
		message += " with filter"
	}
	return message
}
