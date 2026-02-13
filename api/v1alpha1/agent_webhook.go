package v1alpha1

import (
	"context"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ admission.CustomDefaulter = &agentCustomDefaulter{}
var _ admission.CustomValidator = &agentCustomValidator{}

var webhookReader client.Reader

func SetWebhookReader(r client.Reader) {
	webhookReader = r
}

func (r *Agent) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithDefaulter(&agentCustomDefaulter{}).
		WithValidator(&agentCustomValidator{}).
		Complete()
}

type agentCustomDefaulter struct{}

func (d *agentCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	agent, ok := obj.(*Agent)
	if !ok {
		return fmt.Errorf("expected *Agent for defaulting, got %T", obj)
	}
	agent.Default()
	return nil
}

type agentCustomValidator struct{}

func (v *agentCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	agent, ok := obj.(*Agent)
	if !ok {
		return nil, fmt.Errorf("expected *Agent for create validation, got %T", obj)
	}
	return agent.ValidateCreate()
}

func (v *agentCustomValidator) ValidateUpdate(_ context.Context, oldObj runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	agent, ok := newObj.(*Agent)
	if !ok {
		return nil, fmt.Errorf("expected *Agent for update validation, got %T", newObj)
	}
	return agent.ValidateUpdate(oldObj)
}

func (v *agentCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	agent, ok := obj.(*Agent)
	if !ok {
		return nil, fmt.Errorf("expected *Agent for delete validation, got %T", obj)
	}
	return agent.ValidateDelete()
}

func (r *Agent) Default() {
	if r.Spec.Replicas == nil && r.Spec.Autoscaling == nil {
		defaultReplicas := int32(1)
		r.Spec.Replicas = &defaultReplicas
	}
	if r.Spec.SkillResolutionPolicy == "" {
		r.Spec.SkillResolutionPolicy = "Strict"
	}
	if r.Spec.Observability.Enabled == nil {
		v := true
		r.Spec.Observability.Enabled = &v
	}
	if r.Spec.Observability.Metrics.Path == "" {
		r.Spec.Observability.Metrics.Path = "/metrics"
	}
	if r.Spec.Observability.Metrics.Port == 0 {
		r.Spec.Observability.Metrics.Port = 8080
	}
	if r.Spec.ReliabilityPolicyRef == nil {
		r.Spec.ReliabilityPolicyRef = &LocalObjectReference{Name: "default-reliability"}
	}
	if r.Spec.Service.Enabled {
		if r.Spec.Service.Port == 0 {
			r.Spec.Service.Port = 80
		}
		if r.Spec.Service.TargetPort == 0 {
			r.Spec.Service.TargetPort = 8080
		}
	}
}

func (r *Agent) ValidateCreate() (admission.Warnings, error) {
	return nil, r.validateAgent()
}

func (r *Agent) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	return nil, r.validateAgent()
}

func (r *Agent) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}

func (r *Agent) validateAgent() error {
	var allErrs field.ErrorList

	if r.Spec.RuntimeRef.Name == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "runtimeRef", "name"), "must be set"))
	}
	if r.Spec.Replicas != nil && *r.Spec.Replicas < 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "replicas"), *r.Spec.Replicas, "must be >= 0"))
	}
	if r.Spec.SkillResolutionPolicy != "" && !slices.Contains([]string{"Strict", "BestEffort"}, r.Spec.SkillResolutionPolicy) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "skillResolutionPolicy"), r.Spec.SkillResolutionPolicy, "must be Strict or BestEffort"))
	}
	if r.Spec.Observability.Metrics.Port < 0 || r.Spec.Observability.Metrics.Port > 65535 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "observability", "metrics", "port"), r.Spec.Observability.Metrics.Port, "must be in 0..65535"))
	}
	if r.Spec.Autoscaling != nil {
		a := r.Spec.Autoscaling
		if a.MinReplicas < 1 {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "autoscaling", "minReplicas"), a.MinReplicas, "must be >= 1"))
		}
		if a.MaxReplicas < a.MinReplicas {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "autoscaling", "maxReplicas"), a.MaxReplicas, "must be >= minReplicas"))
		}
		if a.TargetCPUUtilizationPercentage < 1 || a.TargetCPUUtilizationPercentage > 100 {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "autoscaling", "targetCPUUtilizationPercentage"), a.TargetCPUUtilizationPercentage, "must be in 1..100"))
		}
	}
	if r.Spec.Service.Enabled {
		if r.Spec.Service.Port < 1 || r.Spec.Service.Port > 65535 {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "service", "port"), r.Spec.Service.Port, "must be in 1..65535"))
		}
		if r.Spec.Service.TargetPort < 1 || r.Spec.Service.TargetPort > 65535 {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "service", "targetPort"), r.Spec.Service.TargetPort, "must be in 1..65535"))
		}
	}

	if webhookReader != nil {
		if err := validateReferences(context.Background(), webhookReader, r); err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec"), r.Spec, err.Error()))
		}
		if err := validateReliabilityPolicy(context.Background(), webhookReader, r); err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "reliabilityPolicyRef"), r.Spec.ReliabilityPolicyRef, err.Error()))
		}
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GroupVersion.WithKind("Agent").GroupKind(), r.Name, allErrs)
}

func validateReferences(ctx context.Context, c client.Reader, a *Agent) error {
	var runtimeObj AgentRuntime
	if err := c.Get(ctx, client.ObjectKey{Namespace: a.Namespace, Name: a.Spec.RuntimeRef.Name}, &runtimeObj); err != nil {
		return fmt.Errorf("runtimeRef %q not resolvable: %w", a.Spec.RuntimeRef.Name, err)
	}

	if a.Spec.LLMEndpointRef != nil && a.Spec.LLMEndpointRef.Name != "" {
		var llm LLMEndpoint
		if err := c.Get(ctx, client.ObjectKey{Namespace: a.Namespace, Name: a.Spec.LLMEndpointRef.Name}, &llm); err != nil {
			return fmt.Errorf("llmEndpointRef %q not resolvable: %w", a.Spec.LLMEndpointRef.Name, err)
		}
		if requiresAuth(llm.Spec.Provider) && llm.Spec.Auth.SecretRef.Name == "" {
			return fmt.Errorf("llmEndpoint %q provider %q requires auth.secretRef.name", llm.Name, llm.Spec.Provider)
		}
	}

	for _, ref := range a.Spec.MemoryRefs {
		var mem MemoryStore
		if err := c.Get(ctx, client.ObjectKey{Namespace: a.Namespace, Name: ref.Name}, &mem); err != nil {
			return fmt.Errorf("memoryRef %q not resolvable: %w", ref.Name, err)
		}
		if requiresMemorySecret(mem.Spec.Type) && (mem.Spec.Connection.SecretRef == nil || mem.Spec.Connection.SecretRef.Name == "") {
			return fmt.Errorf("memoryStore %q type %q requires connection.secretRef.name", mem.Name, mem.Spec.Type)
		}
	}

	for _, ref := range a.Spec.ContextRefs {
		var cp ContextProvider
		if err := c.Get(ctx, client.ObjectKey{Namespace: a.Namespace, Name: ref.Name}, &cp); err != nil {
			return fmt.Errorf("contextRef %q not resolvable: %w", ref.Name, err)
		}
	}
	for _, ref := range a.Spec.SkillRefs {
		var skill AgentSkill
		if err := c.Get(ctx, client.ObjectKey{Namespace: a.Namespace, Name: ref.Name}, &skill); err != nil {
			return fmt.Errorf("skillRef %q not resolvable: %w", ref.Name, err)
		}
		if skill.Spec.Source == nil && skill.Spec.Type == "" {
			return fmt.Errorf("skill %q must set spec.type for manual skills", skill.Name)
		}
		if skill.Spec.Source != nil {
			if skill.Spec.Source.Protocol == "" || skill.Spec.Source.URL == "" {
				return fmt.Errorf("skill %q source requires protocol and url", skill.Name)
			}
		}
	}
	if a.Spec.BudgetPolicyRef != nil && a.Spec.BudgetPolicyRef.Name != "" {
		var bp AgentBudgetPolicy
		if err := c.Get(ctx, client.ObjectKey{Namespace: a.Namespace, Name: a.Spec.BudgetPolicyRef.Name}, &bp); err != nil {
			return fmt.Errorf("budgetPolicyRef %q not resolvable: %w", a.Spec.BudgetPolicyRef.Name, err)
		}
	}

	return nil
}

func validateReliabilityPolicy(ctx context.Context, c client.Reader, a *Agent) error {
	if a.Spec.ReliabilityPolicyRef == nil || a.Spec.ReliabilityPolicyRef.Name == "" {
		return nil
	}
	var policy AgentReliabilityPolicy
	if err := c.Get(ctx, client.ObjectKey{Namespace: a.Namespace, Name: a.Spec.ReliabilityPolicyRef.Name}, &policy); err != nil {
		if a.Spec.ReliabilityPolicyRef.Name == "default-reliability" && apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("reliabilityPolicyRef %q not resolvable: %w", a.Spec.ReliabilityPolicyRef.Name, err)
	}

	p := policy.Spec
	if p.Health.MinReadyPercent < 1 || p.Health.MinReadyPercent > 100 {
		return fmt.Errorf("policy %q health.minReadyPercent must be in 1..100", policy.Name)
	}
	if p.Remediation.MaxActionsPerHour < 1 {
		return fmt.Errorf("policy %q remediation.maxActionsPerHour must be >= 1", policy.Name)
	}
	if p.Remediation.CooldownSeconds < 0 {
		return fmt.Errorf("policy %q remediation.cooldownSeconds must be >= 0", policy.Name)
	}
	allowed := []string{"RestartPods", "ScaleOut", "TrafficDrain", "RollbackRevision", "FailOpenReadOnly"}
	seen := map[string]struct{}{}
	for _, action := range p.Remediation.Actions {
		if !slices.Contains(allowed, action) {
			return fmt.Errorf("policy %q remediation action %q is not supported", policy.Name, action)
		}
		if _, ok := seen[action]; ok {
			return fmt.Errorf("policy %q remediation action %q duplicated", policy.Name, action)
		}
		seen[action] = struct{}{}
	}
	return nil
}

func requiresAuth(provider string) bool {
	switch provider {
	case "vllm", "ollama":
		return false
	default:
		return true
	}
}

func requiresMemorySecret(t string) bool {
	switch t {
	case "redis", "postgres", "qdrant", "milvus", "pinecone":
		return true
	default:
		return false
	}
}
