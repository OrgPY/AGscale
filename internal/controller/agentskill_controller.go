package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

type AgentSkillReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *AgentSkillReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	started := time.Now()
	result := "success"
	protocol := "manual"
	defer func() {
		observeReconcile("AgentSkill", result, started)
	}()

	skill := &agscalev1alpha1.AgentSkill{}
	if err := r.Get(ctx, req.NamespacedName, skill); err != nil {
		result = "not_found"
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if skill.Spec.Source == nil {
		now := metav1.Now()
		skill.Status.ObservedGeneration = skill.Generation
		skill.Status.SourceResolved = true
		skill.Status.LastResolvedAt = &now
		skill.Status.LastError = ""
		setSkillCondition(&skill.Status.Conditions, "Ready", metav1.ConditionTrue, "ManualSkill", "manual skill is ready")
		if err := r.Status().Update(ctx, skill); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("AgentSkill", "StatusUpdate").Inc()
			return ctrl.Result{}, err
		}
		skillResolutionTotal.WithLabelValues(protocol, "success").Inc()
		skillResolutionDurationSeconds.WithLabelValues(protocol).Observe(time.Since(started).Seconds())
		return ctrl.Result{}, nil
	}
	protocol = skill.Spec.Source.Protocol

	applySkillDefaults(skill)
	trackLatest := skill.Spec.SyncPolicy.Mode == "TrackLatest"
	if !trackLatest && skill.Status.SourceResolved && skill.Status.ObservedGeneration == skill.Generation {
		return ctrl.Result{}, nil
	}

	resolved, version, err := r.resolveSkillFromSource(ctx, skill)
	if err != nil {
		now := metav1.Now()
		skill.Status.ObservedGeneration = skill.Generation
		skill.Status.SourceResolved = false
		skill.Status.LastResolvedAt = &now
		skill.Status.LastError = err.Error()
		setSkillCondition(&skill.Status.Conditions, "Ready", metav1.ConditionFalse, "ResolveFailed", err.Error())
		setSkillCondition(&skill.Status.Conditions, "Degraded", metav1.ConditionTrue, "ResolveFailed", err.Error())
		if r.Recorder != nil {
			r.Recorder.Eventf(skill, corev1.EventTypeWarning, "SkillResolveFailed", "skill resolution failed: %v", err)
		}
		if statusErr := r.Status().Update(ctx, skill); statusErr != nil {
			logger.Error(statusErr, "failed to update skill status")
			result = "error"
			reconcileErrorsTotal.WithLabelValues("AgentSkill", "StatusUpdate").Inc()
			return ctrl.Result{}, statusErr
		}
		result = "error"
		reconcileErrorsTotal.WithLabelValues("AgentSkill", "ResolveFailed").Inc()
		skillResolutionTotal.WithLabelValues(protocol, "error").Inc()
		skillResolutionDurationSeconds.WithLabelValues(protocol).Observe(time.Since(started).Seconds())
		return ctrl.Result{RequeueAfter: time.Duration(skill.Spec.SyncPolicy.IntervalSeconds) * time.Second}, nil
	}

	changed := false
	if resolved.Type != "" && skill.Spec.Type != resolved.Type {
		skill.Spec.Type = resolved.Type
		changed = true
	}
	if resolved.Description != "" && skill.Spec.Description != resolved.Description {
		skill.Spec.Description = resolved.Description
		changed = true
	}
	if resolved.Endpoint != "" && skill.Spec.Endpoint != resolved.Endpoint {
		skill.Spec.Endpoint = resolved.Endpoint
		changed = true
	}
	if resolved.Config != nil {
		skill.Spec.Config = resolved.Config
		changed = true
	}

	if changed {
		if err := r.Update(ctx, skill); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("AgentSkill", "SpecUpdate").Inc()
			return ctrl.Result{}, err
		}
	}

	now := metav1.Now()
	skill.Status.ObservedGeneration = skill.Generation
	skill.Status.SourceResolved = true
	skill.Status.ResolvedVersion = version
	skill.Status.LastResolvedAt = &now
	skill.Status.LastError = ""
	setSkillCondition(&skill.Status.Conditions, "Ready", metav1.ConditionTrue, "Resolved", "skill source resolved successfully")
	setSkillCondition(&skill.Status.Conditions, "Degraded", metav1.ConditionFalse, "Healthy", "skill source healthy")
	if r.Recorder != nil {
		r.Recorder.Eventf(skill, corev1.EventTypeNormal, "SkillResolved", "resolved skill from %s", skill.Spec.Source.Protocol)
	}
	if err := r.Status().Update(ctx, skill); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("AgentSkill", "StatusUpdate").Inc()
		return ctrl.Result{}, err
	}
	skillResolutionTotal.WithLabelValues(protocol, "success").Inc()
	skillResolutionDurationSeconds.WithLabelValues(protocol).Observe(time.Since(started).Seconds())

	if trackLatest {
		return ctrl.Result{RequeueAfter: time.Duration(skill.Spec.SyncPolicy.IntervalSeconds) * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

type resolvedSkill struct {
	Type        string
	Description string
	Endpoint    string
	Config      map[string]string
}

func (r *AgentSkillReconciler) resolveSkillFromSource(ctx context.Context, skill *agscalev1alpha1.AgentSkill) (resolvedSkill, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, skill.Spec.Source.URL, nil)
	if err != nil {
		return resolvedSkill{}, "", err
	}

	if skill.Spec.Source.AuthSecretRef != nil && skill.Spec.Source.AuthSecretRef.Name != "" {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: skill.Namespace, Name: skill.Spec.Source.AuthSecretRef.Name}, secret); err != nil {
			return resolvedSkill{}, "", fmt.Errorf("unable to read auth secret: %w", err)
		}
		key := skill.Spec.Source.AuthSecretRef.Key
		if key == "" {
			key = "token"
		}
		token, ok := secret.Data[key]
		if !ok {
			return resolvedSkill{}, "", fmt.Errorf("auth secret key %q missing", key)
		}
		req.Header.Set("Authorization", "Bearer "+string(token))
	}

	clientHTTP := &http.Client{Timeout: 8 * time.Second}
	resp, err := clientHTTP.Do(req)
	if err != nil {
		return resolvedSkill{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resolvedSkill{}, "", fmt.Errorf("source returned status %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return resolvedSkill{}, "", err
	}

	switch skill.Spec.Source.Protocol {
	case "openapi-http":
		return parseOpenAPISkill(payload)
	case "mcp":
		return parseMCPSkill(payload)
	default:
		return resolvedSkill{}, "", fmt.Errorf("unsupported source protocol %q", skill.Spec.Source.Protocol)
	}
}

func parseOpenAPISkill(payload map[string]any) (resolvedSkill, string, error) {
	out := resolvedSkill{
		Type:        stringValue(payload["type"]),
		Description: stringValue(payload["description"]),
		Endpoint:    stringValue(payload["endpoint"]),
		Config:      mapValue(payload["config"]),
	}
	version := stringValue(payload["version"])
	if out.Type == "" {
		out.Type = "http-tool"
	}
	if out.Endpoint == "" {
		return resolvedSkill{}, "", fmt.Errorf("openapi source missing endpoint")
	}
	return out, version, nil
}

func parseMCPSkill(payload map[string]any) (resolvedSkill, string, error) {
	if tool, ok := payload["tool"].(map[string]any); ok {
		out := resolvedSkill{
			Type:        firstNonEmpty(stringValue(tool["type"]), "mcp-tool"),
			Description: stringValue(tool["description"]),
			Endpoint:    stringValue(tool["endpoint"]),
			Config:      mapValue(tool["config"]),
		}
		if out.Endpoint == "" {
			out.Endpoint = stringValue(payload["endpoint"])
		}
		if out.Endpoint == "" {
			return resolvedSkill{}, "", fmt.Errorf("mcp source missing endpoint")
		}
		return out, stringValue(payload["version"]), nil
	}

	out := resolvedSkill{
		Type:        firstNonEmpty(stringValue(payload["type"]), "mcp-tool"),
		Description: stringValue(payload["description"]),
		Endpoint:    stringValue(payload["endpoint"]),
		Config:      mapValue(payload["config"]),
	}
	if out.Endpoint == "" {
		return resolvedSkill{}, "", fmt.Errorf("mcp source missing endpoint")
	}
	return out, stringValue(payload["version"]), nil
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func mapValue(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		out[k] = fmt.Sprintf("%v", val)
	}
	return out
}

func firstNonEmpty(v string, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func applySkillDefaults(skill *agscalev1alpha1.AgentSkill) {
	if skill.Spec.SyncPolicy.Mode == "" {
		skill.Spec.SyncPolicy.Mode = "Snapshot"
	}
	if skill.Spec.SyncPolicy.IntervalSeconds == 0 {
		skill.Spec.SyncPolicy.IntervalSeconds = 300
	}
	if skill.Spec.Source != nil {
		if skill.Spec.Source.Protocol == "" {
			skill.Spec.Source.Protocol = "openapi-http"
		}
		if skill.Spec.Source.URL == "" && skill.Spec.Source.ExternalID != "" {
			skill.Spec.Source.URL = "https://clawhub.ai/skills/" + skill.Spec.Source.ExternalID
		}
	}
}

func setSkillCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *AgentSkillReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agscalev1alpha1.AgentSkill{}).
		Complete(r)
}
