package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

type AgentReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	started := time.Now()
	result := "success"
	defer func() {
		observeReconcile("Agent", result, started)
	}()

	agent := &agscalev1alpha1.Agent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		result = "not_found"
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	runtimeObj := &agscalev1alpha1.AgentRuntime{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: agent.Namespace, Name: agent.Spec.RuntimeRef.Name}, runtimeObj); err != nil {
		r.setCondition(agent, "ReferencesResolved", metav1.ConditionFalse, "RuntimeNotFound", err.Error())
		_ = r.Status().Update(ctx, agent)
		result = "error"
		reconcileErrorsTotal.WithLabelValues("Agent", "RuntimeNotFound").Inc()
		return ctrl.Result{}, err
	}

	injectedEnv, err := r.resolveReferences(ctx, agent)
	if err != nil {
		r.setCondition(agent, "ReferencesResolved", metav1.ConditionFalse, "ReferenceResolveFailed", err.Error())
		r.setCondition(agent, "DependencyHealthy", metav1.ConditionFalse, "ReferenceResolveFailed", err.Error())
		_ = r.Status().Update(ctx, agent)
		result = "error"
		reconcileErrorsTotal.WithLabelValues("Agent", "ReferenceResolveFailed").Inc()
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	r.setCondition(agent, "DependencyHealthy", metav1.ConditionTrue, "DependenciesOK", "all dependency references resolved")

	policy, err := r.resolveReliabilityPolicy(ctx, agent)
	if err != nil {
		r.setCondition(agent, "Healthy", metav1.ConditionFalse, "PolicyResolveFailed", err.Error())
		_ = r.Status().Update(ctx, agent)
		result = "error"
		reconcileErrorsTotal.WithLabelValues("Agent", "PolicyResolveFailed").Inc()
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	deploy := buildDeployment(agent, runtimeObj, injectedEnv)
	if err := controllerutil.SetControllerReference(agent, deploy, r.Scheme); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("Agent", "SetControllerReference").Inc()
		return ctrl.Result{}, err
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		deploy.Spec = buildDeployment(agent, runtimeObj, injectedEnv).Spec
		return nil
	}); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("Agent", "DeploymentReconcile").Inc()
		return ctrl.Result{}, err
	}

	if agent.Spec.Service.Enabled {
		svc := buildService(agent)
		if err := controllerutil.SetControllerReference(agent, svc, r.Scheme); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("Agent", "SetServiceOwner").Inc()
			return ctrl.Result{}, err
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
			svc.Spec = buildService(agent).Spec
			return nil
		}); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("Agent", "ServiceReconcile").Inc()
			return ctrl.Result{}, err
		}
		agent.Status.ServiceName = svc.Name
		agent.Status.URL = fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", svc.Name, svc.Namespace, svc.Spec.Ports[0].Port)
	}

	if agent.Spec.Autoscaling != nil {
		hpa := buildHPA(agent)
		if err := controllerutil.SetControllerReference(agent, hpa, r.Scheme); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("Agent", "SetHPAOwner").Inc()
			return ctrl.Result{}, err
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, hpa, func() error {
			hpa.Spec = buildHPA(agent).Spec
			return nil
		}); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("Agent", "HPAReconcile").Inc()
			return ctrl.Result{}, err
		}
	} else {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: agent.Name, Namespace: agent.Namespace},
		}
		if err := r.Delete(ctx, hpa); err != nil && !apierrors.IsNotFound(err) {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("Agent", "HPADelete").Inc()
			return ctrl.Result{}, err
		}
	}

	var current appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, &current); err == nil {
		agent.Status.ReadyReplicas = current.Status.ReadyReplicas
		r.evaluateAndRemediate(ctx, agent, &current, policy)
		agent.Status.ObservabilityLastScrapeReady = isObservabilityEnabled(agent)
		agent.Status.ObservabilityLastTelemetryError = ""
	}
	agent.Status.ObservedGeneration = agent.Generation
	r.setCondition(agent, "ReferencesResolved", metav1.ConditionTrue, "ReferencesOK", "all references resolved")
	r.setCondition(agent, "Ready", metav1.ConditionTrue, "ResourcesReconciled", "deployment/service/hpa reconciled")

	if err := r.Status().Update(ctx, agent); err != nil {
		logger.Error(err, "unable to update Agent status")
		result = "error"
		reconcileErrorsTotal.WithLabelValues("Agent", "StatusUpdate").Inc()
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *AgentReconciler) resolveReliabilityPolicy(ctx context.Context, agent *agscalev1alpha1.Agent) (*agscalev1alpha1.AgentReliabilityPolicy, error) {
	name := "default-reliability"
	if agent.Spec.ReliabilityPolicyRef != nil && agent.Spec.ReliabilityPolicyRef.Name != "" {
		name = agent.Spec.ReliabilityPolicyRef.Name
	}
	policy := &agscalev1alpha1.AgentReliabilityPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: agent.Namespace, Name: name}, policy); err != nil {
		if name == "default-reliability" && apierrors.IsNotFound(err) {
			policy = &agscalev1alpha1.AgentReliabilityPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "default-reliability", Namespace: agent.Namespace},
			}
			applyPolicyDefaults(policy)
			return policy, nil
		}
		return nil, err
	}
	applyPolicyDefaults(policy)
	return policy, nil
}

func (r *AgentReconciler) evaluateAndRemediate(ctx context.Context, agent *agscalev1alpha1.Agent, deploy *appsv1.Deployment, policy *agscalev1alpha1.AgentReliabilityPolicy) {
	desired := int32(1)
	if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas > 0 {
		desired = *deploy.Spec.Replicas
	}
	readyPercent := int32(100 * deploy.Status.ReadyReplicas / desired)
	if readyPercent >= policy.Spec.Health.MinReadyPercent {
		r.setCondition(agent, "Healthy", metav1.ConditionTrue, "MinReadyMet", fmt.Sprintf("ready=%d%%", readyPercent))
		r.setCondition(agent, "ErrorBudgetBurning", metav1.ConditionFalse, "WithinSLO", "ready ratio above threshold")
		r.setCondition(agent, "Remediating", metav1.ConditionFalse, "Idle", "no remediation needed")
		return
	}

	r.setCondition(agent, "Healthy", metav1.ConditionFalse, "MinReadyNotMet", fmt.Sprintf("ready=%d%% below min=%d%%", readyPercent, policy.Spec.Health.MinReadyPercent))
	r.setCondition(agent, "ErrorBudgetBurning", metav1.ConditionTrue, "ReadinessDrop", "readiness below policy threshold")

	action, remediated, reason := r.runRemediationChain(ctx, agent, deploy, policy)
	if remediated {
		r.recordRemediation(agent, action, reason)
		agentRemediationTotal.WithLabelValues(action, "applied").Inc()
		r.setCondition(agent, "Remediating", metav1.ConditionTrue, "ActionApplied", fmt.Sprintf("%s: %s", action, reason))
		if r.Recorder != nil {
			r.Recorder.Eventf(agent, corev1.EventTypeWarning, "RemediationApplied", "%s: %s", action, reason)
		}
		return
	}
	agentRemediationTotal.WithLabelValues("none", "skipped").Inc()
	r.setCondition(agent, "Remediating", metav1.ConditionFalse, "NoAction", reason)
}

func (r *AgentReconciler) runRemediationChain(ctx context.Context, agent *agscalev1alpha1.Agent, deploy *appsv1.Deployment, policy *agscalev1alpha1.AgentReliabilityPolicy) (string, bool, string) {
	if !canRemediate(agent, policy, time.Now()) {
		return "", false, "cooldown or rate limit active"
	}
	for _, action := range policy.Spec.Remediation.Actions {
		ok, reason := r.executeAction(ctx, action, agent, deploy)
		if ok {
			return action, true, reason
		}
	}
	return "", false, "no action succeeded"
}

func (r *AgentReconciler) executeAction(ctx context.Context, action string, agent *agscalev1alpha1.Agent, deploy *appsv1.Deployment) (bool, string) {
	switch action {
	case "RestartPods":
		var pods corev1.PodList
		if err := r.List(ctx, &pods, client.InNamespace(agent.Namespace), client.MatchingLabels{"app.kubernetes.io/instance": agent.Name}); err != nil {
			return false, err.Error()
		}
		if len(pods.Items) == 0 {
			return false, "no pods to restart"
		}
		if err := r.Delete(ctx, &pods.Items[0]); err != nil {
			return false, err.Error()
		}
		return true, "deleted one pod to trigger restart"
	case "ScaleOut":
		if deploy.Spec.Replicas == nil {
			return false, "deployment replicas unset"
		}
		current := *deploy.Spec.Replicas
		max := current + 1
		if agent.Spec.Autoscaling != nil && agent.Spec.Autoscaling.MaxReplicas > 0 {
			max = agent.Spec.Autoscaling.MaxReplicas
		}
		if current >= max {
			return false, "already at max replicas"
		}
		next := current + 1
		deploy.Spec.Replicas = &next
		if err := r.Update(ctx, deploy); err != nil {
			return false, err.Error()
		}
		return true, fmt.Sprintf("scaled deployment from %d to %d", current, next)
	case "FailOpenReadOnly":
		if deploy.Spec.Template.Annotations == nil {
			deploy.Spec.Template.Annotations = map[string]string{}
		}
		deploy.Spec.Template.Annotations["agscale.io/degraded-mode"] = "read-only"
		if err := r.Update(ctx, deploy); err != nil {
			return false, err.Error()
		}
		return true, "set degraded read-only annotation"
	case "TrafficDrain":
		return true, "traffic drain action configured as no-op in v1alpha1"
	case "RollbackRevision":
		return true, "rollback action configured as no-op in v1alpha1"
	default:
		return false, "unsupported action"
	}
}

func (r *AgentReconciler) recordRemediation(agent *agscalev1alpha1.Agent, action, reason string) {
	now := metav1.Now()
	agent.Status.LastRemediationAction = action
	agent.Status.LastRemediationTime = &now
	agent.Status.RecentRemediations = append(agent.Status.RecentRemediations, agscalev1alpha1.RemediationRecord{
		Action:    action,
		Reason:    reason,
		Timestamp: now,
	})
	if len(agent.Status.RecentRemediations) > 20 {
		agent.Status.RecentRemediations = agent.Status.RecentRemediations[len(agent.Status.RecentRemediations)-20:]
	}
}

func canRemediate(agent *agscalev1alpha1.Agent, policy *agscalev1alpha1.AgentReliabilityPolicy, now time.Time) bool {
	cooldown := time.Duration(policy.Spec.Remediation.CooldownSeconds) * time.Second
	if agent.Status.LastRemediationTime != nil && cooldown > 0 {
		last := agent.Status.LastRemediationTime.Time
		if now.Sub(last) < cooldown {
			return false
		}
	}
	limit := int(policy.Spec.Remediation.MaxActionsPerHour)
	if limit <= 0 {
		return false
	}
	count := 0
	for _, rec := range agent.Status.RecentRemediations {
		if now.Sub(rec.Timestamp.Time) <= time.Hour {
			count++
		}
	}
	return count < limit
}

func applyPolicyDefaults(policy *agscalev1alpha1.AgentReliabilityPolicy) {
	if policy.Spec.Health.MinReadyPercent == 0 {
		policy.Spec.Health.MinReadyPercent = 80
	}
	if policy.Spec.Health.StartupGraceSeconds == 0 {
		policy.Spec.Health.StartupGraceSeconds = 120
	}
	if policy.Spec.Health.FailureWindowSeconds == 0 {
		policy.Spec.Health.FailureWindowSeconds = 300
	}
	if policy.Spec.Remediation.CooldownSeconds == 0 {
		policy.Spec.Remediation.CooldownSeconds = 300
	}
	if policy.Spec.Remediation.MaxActionsPerHour == 0 {
		policy.Spec.Remediation.MaxActionsPerHour = 6
	}
	if len(policy.Spec.Remediation.Actions) == 0 {
		policy.Spec.Remediation.Actions = []string{"RestartPods", "ScaleOut", "RollbackRevision", "FailOpenReadOnly"}
	}
}

func (r *AgentReconciler) resolveReferences(ctx context.Context, agent *agscalev1alpha1.Agent) ([]corev1.EnvVar, error) {
	env := []corev1.EnvVar{
		{Name: "AGSCALE_AGENT_NAME", Value: agent.Name},
	}

	if agent.Spec.LLMEndpointRef != nil && agent.Spec.LLMEndpointRef.Name != "" {
		var llm agscalev1alpha1.LLMEndpoint
		if err := r.Get(ctx, types.NamespacedName{Namespace: agent.Namespace, Name: agent.Spec.LLMEndpointRef.Name}, &llm); err != nil {
			return nil, err
		}
		env = append(env,
			corev1.EnvVar{Name: "AGSCALE_LLM_PROVIDER", Value: llm.Spec.Provider},
			corev1.EnvVar{Name: "AGSCALE_LLM_ENDPOINT", Value: llm.Spec.BaseURL},
			corev1.EnvVar{Name: "AGSCALE_LLM_MODEL", Value: llm.Spec.DefaultModel},
		)
	}

	memoryNames := make([]string, 0, len(agent.Spec.MemoryRefs))
	for _, ref := range agent.Spec.MemoryRefs {
		var mem agscalev1alpha1.MemoryStore
		if err := r.Get(ctx, types.NamespacedName{Namespace: agent.Namespace, Name: ref.Name}, &mem); err != nil {
			return nil, err
		}
		memoryNames = append(memoryNames, mem.Name)
	}
	if len(memoryNames) > 0 {
		env = append(env, corev1.EnvVar{Name: "AGSCALE_MEMORY_REFS", Value: strings.Join(memoryNames, ",")})
	}

	contextNames := make([]string, 0, len(agent.Spec.ContextRefs))
	for _, ref := range agent.Spec.ContextRefs {
		var cp agscalev1alpha1.ContextProvider
		if err := r.Get(ctx, types.NamespacedName{Namespace: agent.Namespace, Name: ref.Name}, &cp); err != nil {
			return nil, err
		}
		contextNames = append(contextNames, cp.Name)
	}
	if len(contextNames) > 0 {
		env = append(env, corev1.EnvVar{Name: "AGSCALE_CONTEXT_REFS", Value: strings.Join(contextNames, ",")})
	}
	skillNames := make([]string, 0, len(agent.Spec.SkillRefs))
	skillEndpoints := map[string]string{}
	skillProtocols := map[string]string{}
	skillConfigs := map[string]map[string]string{}
	strictResolution := agent.Spec.SkillResolutionPolicy != "BestEffort"
	for _, ref := range agent.Spec.SkillRefs {
		var skill agscalev1alpha1.AgentSkill
		if err := r.Get(ctx, types.NamespacedName{Namespace: agent.Namespace, Name: ref.Name}, &skill); err != nil {
			return nil, err
		}
		if strictResolution && skill.Spec.Source != nil && !skill.Status.SourceResolved {
			return nil, fmt.Errorf("skill %q is not source-resolved yet", skill.Name)
		}
		skillNames = append(skillNames, skill.Name)
		if skill.Spec.Endpoint != "" {
			skillEndpoints[skill.Name] = skill.Spec.Endpoint
		}
		if skill.Spec.Source != nil && skill.Spec.Source.Protocol != "" {
			skillProtocols[skill.Name] = skill.Spec.Source.Protocol
		}
		if len(skill.Spec.Config) > 0 {
			skillConfigs[skill.Name] = skill.Spec.Config
		}
	}
	if len(skillNames) > 0 {
		env = append(env, corev1.EnvVar{Name: "AGSCALE_SKILL_REFS", Value: strings.Join(skillNames, ",")})
		if v, err := json.Marshal(skillEndpoints); err == nil {
			env = append(env, corev1.EnvVar{Name: "AGSCALE_SKILL_ENDPOINTS_JSON", Value: string(v)})
		}
		if v, err := json.Marshal(skillProtocols); err == nil {
			env = append(env, corev1.EnvVar{Name: "AGSCALE_SKILL_PROTOCOLS_JSON", Value: string(v)})
		}
		if v, err := json.Marshal(skillConfigs); err == nil {
			env = append(env, corev1.EnvVar{Name: "AGSCALE_SKILL_CONFIG_JSON", Value: string(v)})
		}
	}

	return env, nil
}

func (r *AgentReconciler) setCondition(agent *agscalev1alpha1.Agent, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: agent.Generation,
	}
	meta.SetStatusCondition(&agent.Status.Conditions, condition)
	agentConditionTransitionsTotal.WithLabelValues(conditionType, string(status)).Inc()
}

func buildDeployment(agent *agscalev1alpha1.Agent, runtimeObj *agscalev1alpha1.AgentRuntime, injected []corev1.EnvVar) *appsv1.Deployment {
	labels := map[string]string{
		"app.kubernetes.io/name":       "agscale-agent",
		"app.kubernetes.io/instance":   agent.Name,
		"app.kubernetes.io/managed-by": "agscale-controller",
	}
	for k, v := range runtimeObj.Spec.PodLabels {
		labels[k] = v
	}
	for k, v := range agent.Spec.PodLabels {
		labels[k] = v
	}
	labels["agscale.io/agent"] = agent.Name
	if agent.Spec.Observability.AlertProfile != "" {
		labels["agscale.io/alert-profile"] = agent.Spec.Observability.AlertProfile
	}

	annotations := map[string]string{}
	for k, v := range runtimeObj.Spec.PodAnnotations {
		annotations[k] = v
	}
	for k, v := range agent.Spec.PodAnnotations {
		annotations[k] = v
	}
	if isObservabilityEnabled(agent) {
		annotations["prometheus.io/scrape"] = "true"
		annotations["prometheus.io/path"] = firstNonEmptyString(agent.Spec.Observability.Metrics.Path, "/metrics")
		annotations["prometheus.io/port"] = fmt.Sprintf("%d", firstNonZeroInt32(agent.Spec.Observability.Metrics.Port, 8080))
	}

	replicas := int32(1)
	if agent.Spec.Replicas != nil {
		replicas = *agent.Spec.Replicas
	} else if agent.Spec.Autoscaling != nil && agent.Spec.Autoscaling.MinReplicas > 0 {
		replicas = agent.Spec.Autoscaling.MinReplicas
	}

	image := runtimeObj.Spec.Image
	if agent.Spec.Image != "" {
		image = agent.Spec.Image
	}
	pullPolicy := runtimeObj.Spec.ImagePullPolicy
	if agent.Spec.ImagePullPolicy != "" {
		pullPolicy = agent.Spec.ImagePullPolicy
	}
	command := runtimeObj.Spec.Command
	if len(agent.Spec.Command) > 0 {
		command = agent.Spec.Command
	}
	args := runtimeObj.Spec.Args
	if len(agent.Spec.Args) > 0 {
		args = agent.Spec.Args
	}
	env := append(append([]corev1.EnvVar{}, runtimeObj.Spec.Env...), agent.Spec.Env...)
	env = append(env, injected...)
	if isObservabilityEnabled(agent) {
		env = append(env,
			corev1.EnvVar{Name: "OTEL_SERVICE_NAME", Value: fmt.Sprintf("agent-%s", agent.Name)},
			corev1.EnvVar{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: fmt.Sprintf("service.name=agent-%s,k8s.namespace.name=%s,agscale.agent=%s", agent.Name, agent.Namespace, agent.Name)},
		)
		if agent.Spec.Observability.Tracing.Enabled && agent.Spec.Observability.Tracing.OTLPEndpoint != "" {
			env = append(env, corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: agent.Spec.Observability.Tracing.OTLPEndpoint})
		}
	}
	resources := runtimeObj.Spec.Resources
	if !isEmptyResources(agent.Spec.Resources) {
		resources = agent.Spec.Resources
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app.kubernetes.io/instance": agent.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "agent",
							Image:           image,
							ImagePullPolicy: pullPolicy,
							Command:         command,
							Args:            args,
							Env:             env,
							Ports:           runtimeObj.Spec.Ports,
							Resources:       resources,
							LivenessProbe:   runtimeObj.Spec.LivenessProbe,
							ReadinessProbe:  runtimeObj.Spec.ReadinessProbe,
							SecurityContext: runtimeObj.Spec.SecurityContext,
						},
					},
				},
			},
		},
	}
}

func isObservabilityEnabled(agent *agscalev1alpha1.Agent) bool {
	if agent.Spec.Observability.Enabled == nil {
		return true
	}
	return *agent.Spec.Observability.Enabled
}

func firstNonEmptyString(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func firstNonZeroInt32(v, fallback int32) int32 {
	if v != 0 {
		return v
	}
	return fallback
}

func buildService(agent *agscalev1alpha1.Agent) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: agent.Spec.Service.Type,
			Selector: map[string]string{
				"app.kubernetes.io/instance": agent.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       agent.Spec.Service.Port,
					TargetPort: intstr.FromInt32(agent.Spec.Service.TargetPort),
				},
			},
		},
	}
}

func buildHPA(agent *agscalev1alpha1.Agent) *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := agent.Spec.Autoscaling.MinReplicas
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       agent.Name,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: agent.Spec.Autoscaling.MaxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &agent.Spec.Autoscaling.TargetCPUUtilizationPercentage,
						},
					},
				},
			},
		},
	}
}

func isEmptyResources(r corev1.ResourceRequirements) bool {
	return len(r.Limits) == 0 && len(r.Requests) == 0
}

func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agscalev1alpha1.Agent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Complete(r)
}
