package v1alpha1

import (
	"context"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ admission.CustomDefaulter = &agentBudgetPolicyCustomDefaulter{}
var _ admission.CustomValidator = &agentBudgetPolicyCustomValidator{}

func (r *AgentBudgetPolicy) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithDefaulter(&agentBudgetPolicyCustomDefaulter{}).
		WithValidator(&agentBudgetPolicyCustomValidator{}).
		Complete()
}

type agentBudgetPolicyCustomDefaulter struct{}

func (d *agentBudgetPolicyCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	p, ok := obj.(*AgentBudgetPolicy)
	if !ok {
		return fmt.Errorf("expected *AgentBudgetPolicy for defaulting, got %T", obj)
	}
	p.Default()
	return nil
}

type agentBudgetPolicyCustomValidator struct{}

func (v *agentBudgetPolicyCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	p, ok := obj.(*AgentBudgetPolicy)
	if !ok {
		return nil, fmt.Errorf("expected *AgentBudgetPolicy for create validation, got %T", obj)
	}
	return nil, p.validate()
}

func (v *agentBudgetPolicyCustomValidator) ValidateUpdate(_ context.Context, _ runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	p, ok := newObj.(*AgentBudgetPolicy)
	if !ok {
		return nil, fmt.Errorf("expected *AgentBudgetPolicy for update validation, got %T", newObj)
	}
	return nil, p.validate()
}

func (v *agentBudgetPolicyCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (r *AgentBudgetPolicy) Default() {
	if r.Spec.Scope == "" {
		r.Spec.Scope = BudgetScopeAgent
	}
	if r.Spec.Window.Mode == "" {
		r.Spec.Window.Mode = BudgetWindowRolling24h
	}
	if r.Spec.Evaluation.IntervalSeconds == 0 {
		r.Spec.Evaluation.IntervalSeconds = 60
	}
	if r.Spec.Evaluation.StaleAfterSeconds == 0 {
		r.Spec.Evaluation.StaleAfterSeconds = 300
	}
	if r.Spec.UsageSources.Metrics.Enabled == nil {
		v := true
		r.Spec.UsageSources.Metrics.Enabled = &v
	}
	if r.Spec.Alerting.EmitK8sEvents == nil {
		v := true
		r.Spec.Alerting.EmitK8sEvents = &v
	}
	if r.Spec.Alerting.SetConditions == nil {
		v := true
		r.Spec.Alerting.SetConditions = &v
	}
}

func (r *AgentBudgetPolicy) validate() error {
	var allErrs field.ErrorList
	if r.Spec.Scope != BudgetScopeAgent && r.Spec.Scope != BudgetScopeNamespace {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "scope"), r.Spec.Scope, "must be Agent or Namespace"))
	}
	if r.Spec.Scope == BudgetScopeAgent && r.Spec.TargetRef.Name == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "targetRef", "name"), "must be set when scope=Agent"))
	}
	if r.Spec.Scope == BudgetScopeNamespace && r.Spec.TargetRef.Name == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "targetRef", "name"), "must be set to namespace name when scope=Namespace"))
	}
	if r.Spec.Window.Mode != BudgetWindowRolling24h {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "window", "mode"), r.Spec.Window.Mode, "only Rolling24h is supported in v1alpha1"))
	}
	if r.Spec.Evaluation.IntervalSeconds < 15 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "evaluation", "intervalSeconds"), r.Spec.Evaluation.IntervalSeconds, "must be >= 15"))
	}
	if r.Spec.Evaluation.StaleAfterSeconds < r.Spec.Evaluation.IntervalSeconds {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "evaluation", "staleAfterSeconds"), r.Spec.Evaluation.StaleAfterSeconds, "must be >= intervalSeconds"))
	}
	if r.Spec.Limits.MaxRequests24h == nil && r.Spec.Limits.MaxTokens24h == nil && r.Spec.Limits.MaxEstimatedCostUSD24h == nil {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "limits"), r.Spec.Limits, "at least one limit must be set"))
	}
	if r.Spec.Limits.MaxRequests24h != nil && *r.Spec.Limits.MaxRequests24h < 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "limits", "maxRequests24h"), *r.Spec.Limits.MaxRequests24h, "must be >= 0"))
	}
	if r.Spec.Limits.MaxTokens24h != nil && *r.Spec.Limits.MaxTokens24h < 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "limits", "maxTokens24h"), *r.Spec.Limits.MaxTokens24h, "must be >= 0"))
	}
	if r.Spec.Limits.MaxEstimatedCostUSD24h != nil && *r.Spec.Limits.MaxEstimatedCostUSD24h < 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "limits", "maxEstimatedCostUSD24h"), *r.Spec.Limits.MaxEstimatedCostUSD24h, "must be >= 0"))
	}
	allowedProviderTypes := []string{"openai", "openclaw", "opencode", "mcp"}
	for i, p := range r.Spec.UsageSources.Providers {
		if p.Type == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "usageSources", "providers").Index(i).Child("type"), "must be set"))
			continue
		}
		if !slices.Contains(allowedProviderTypes, p.Type) {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "usageSources", "providers").Index(i).Child("type"), p.Type, "must be one of openai,openclaw,opencode,mcp"))
		}
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GroupVersion.WithKind("AgentBudgetPolicy").GroupKind(), r.Name, allErrs)
}
