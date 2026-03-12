package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
	toolPolicy, err := r.resolveToolPolicy(ctx, agent)
	if err != nil {
		r.setCondition(agent, "ToolPolicyEnforced", metav1.ConditionFalse, "ToolPolicyResolveFailed", err.Error())
		_ = r.Status().Update(ctx, agent)
		result = "error"
		reconcileErrorsTotal.WithLabelValues("Agent", "ToolPolicyResolveFailed").Inc()
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	policy, err := r.resolveReliabilityPolicy(ctx, agent)
	if err != nil {
		r.setCondition(agent, "Healthy", metav1.ConditionFalse, "PolicyResolveFailed", err.Error())
		_ = r.Status().Update(ctx, agent)
		result = "error"
		reconcileErrorsTotal.WithLabelValues("Agent", "PolicyResolveFailed").Inc()
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	if open, until, dep := isDependencyCircuitOpen(agent); open {
		r.setCondition(agent, "DependencyHealthy", metav1.ConditionFalse, "DependencyCircuitOpen", fmt.Sprintf("%s circuit open until %s", dep, until.Format(time.RFC3339)))
		r.setCondition(agent, "ReferencesResolved", metav1.ConditionFalse, "DependencyCircuitOpen", "skipping dependency resolution while circuit is open")
		_ = r.Status().Update(ctx, agent)
		dependencyCircuitTransitionsTotal.WithLabelValues(dep, "open_observed").Inc()
		return ctrl.Result{RequeueAfter: time.Until(until).Round(time.Second)}, nil
	}

	injectedEnv, err := r.resolveReferences(ctx, agent, toolPolicy)
	if err != nil {
		dep := classifyDependencyFromError(err)
		failures := bumpDependencyFailures(agent, dep)
		threshold := dependencyFailureThreshold(policy, dep)
		if threshold > 0 && failures >= threshold {
			setDependencyCircuit(agent, dep, time.Now().Add(time.Duration(policy.Spec.Health.FailureWindowSeconds)*time.Second))
			dependencyCircuitTransitionsTotal.WithLabelValues(dep, "open").Inc()
		}
		if updateErr := r.Update(ctx, agent); updateErr != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("Agent", "DependencyFailureUpdate").Inc()
			return ctrl.Result{}, updateErr
		}
		r.setCondition(agent, "ReferencesResolved", metav1.ConditionFalse, "ReferenceResolveFailed", err.Error())
		r.setCondition(agent, "DependencyHealthy", metav1.ConditionFalse, "ReferenceResolveFailed", err.Error())
		if toolPolicy != nil {
			r.setCondition(agent, "ToolPolicyEnforced", metav1.ConditionFalse, "ToolPolicyDenied", err.Error())
			_ = r.recordToolPolicyViolation(ctx, toolPolicy, err.Error())
		}
		_ = r.Status().Update(ctx, agent)
		result = "error"
		reconcileErrorsTotal.WithLabelValues("Agent", "ReferenceResolveFailed").Inc()
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if clearDependencyFailureState(agent) {
		if updateErr := r.Update(ctx, agent); updateErr != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("Agent", "DependencyFailureClear").Inc()
			return ctrl.Result{}, updateErr
		}
		dependencyCircuitTransitionsTotal.WithLabelValues("any", "closed").Inc()
	}
	r.setCondition(agent, "DependencyHealthy", metav1.ConditionTrue, "DependenciesOK", "all dependency references resolved")
	if toolPolicy != nil {
		r.setCondition(agent, "ToolPolicyEnforced", metav1.ConditionTrue, "ToolPolicyOK", fmt.Sprintf("policy %s enforced", toolPolicy.Name))
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
	} else {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: agent.Name, Namespace: agent.Namespace}}
		if err := r.Delete(ctx, svc); err != nil && !apierrors.IsNotFound(err) {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("Agent", "ServiceDelete").Inc()
			return ctrl.Result{}, err
		}
		agent.Status.ServiceName = ""
		agent.Status.URL = ""
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
	if deploy.Spec.Replicas != nil {
		desired = *deploy.Spec.Replicas
	}
	if desired == 0 {
		r.setCondition(agent, "Healthy", metav1.ConditionTrue, "ScaledToZero", "agent intentionally scaled to zero")
		r.setCondition(agent, "ErrorBudgetBurning", metav1.ConditionFalse, "ScaledToZero", "no traffic expected while scaled to zero")
		r.setCondition(agent, "Remediating", metav1.ConditionFalse, "Idle", "no remediation needed while scaled to zero")
		return
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
	case "SwitchToBackupLLM":
		backupName := agent.GetAnnotations()["agscale.io/backup-llm-endpoint"]
		if backupName == "" {
			llmFailoverTotal.WithLabelValues("skipped").Inc()
			return false, "annotation agscale.io/backup-llm-endpoint not set"
		}
		if agent.Spec.LLMEndpointRef != nil && agent.Spec.LLMEndpointRef.Name == backupName {
			llmFailoverTotal.WithLabelValues("skipped").Inc()
			return false, "backup LLM already active"
		}
		current := &agscalev1alpha1.Agent{}
		if err := r.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, current); err != nil {
			llmFailoverTotal.WithLabelValues("error").Inc()
			return false, err.Error()
		}
		current.Spec.LLMEndpointRef = &agscalev1alpha1.LocalObjectReference{Name: backupName}
		if err := r.Update(ctx, current); err != nil {
			llmFailoverTotal.WithLabelValues("error").Inc()
			return false, err.Error()
		}
		agent.Spec.LLMEndpointRef = &agscalev1alpha1.LocalObjectReference{Name: backupName}
		llmFailoverTotal.WithLabelValues("applied").Inc()
		return true, fmt.Sprintf("switched to backup llm endpoint %q", backupName)
	case "TrafficDrain":
		return true, "traffic drain action configured as no-op in v1alpha1"
	case "RollbackRevision":
		return true, "rollback action configured as no-op in v1alpha1"
	default:
		return false, "unsupported action"
	}
}

const (
	dependencyFailurePrefix = "agscale.io/dependency-failures-"
	dependencyCircuitDepKey = "agscale.io/dependency-circuit-dependency"
	dependencyCircuitUntil  = "agscale.io/dependency-circuit-open-until"
)

func classifyDependencyFromError(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "llmendpoint"), strings.Contains(msg, "llm"):
		return "llm"
	case strings.Contains(msg, "memorystore"), strings.Contains(msg, "memory"):
		return "memory"
	case strings.Contains(msg, "contextprovider"), strings.Contains(msg, "context"):
		return "context"
	case strings.Contains(msg, "agentskill"), strings.Contains(msg, "skill"):
		return "skill"
	default:
		return "unknown"
	}
}

func dependencyFailureThreshold(policy *agscalev1alpha1.AgentReliabilityPolicy, dep string) int32 {
	switch dep {
	case "llm":
		return policy.Spec.DependencyChecks.LLM.ConsecutiveFailuresForOpen
	case "memory":
		return policy.Spec.DependencyChecks.Memory.ConsecutiveFailuresForOpen
	case "context":
		return policy.Spec.DependencyChecks.Context.ConsecutiveFailuresForOpen
	default:
		for _, v := range []int32{
			policy.Spec.DependencyChecks.LLM.ConsecutiveFailuresForOpen,
			policy.Spec.DependencyChecks.Memory.ConsecutiveFailuresForOpen,
			policy.Spec.DependencyChecks.Context.ConsecutiveFailuresForOpen,
		} {
			if v > 0 {
				return v
			}
		}
		return 0
	}
}

func bumpDependencyFailures(agent *agscalev1alpha1.Agent, dep string) int32 {
	if agent.Annotations == nil {
		agent.Annotations = map[string]string{}
	}
	key := dependencyFailurePrefix + dep
	current, _ := strconv.ParseInt(agent.Annotations[key], 10, 32)
	next := int32(current) + 1
	agent.Annotations[key] = strconv.FormatInt(int64(next), 10)
	return next
}

func setDependencyCircuit(agent *agscalev1alpha1.Agent, dep string, until time.Time) {
	if agent.Annotations == nil {
		agent.Annotations = map[string]string{}
	}
	agent.Annotations[dependencyCircuitDepKey] = dep
	agent.Annotations[dependencyCircuitUntil] = until.UTC().Format(time.RFC3339)
}

func isDependencyCircuitOpen(agent *agscalev1alpha1.Agent) (bool, time.Time, string) {
	if len(agent.Annotations) == 0 {
		return false, time.Time{}, ""
	}
	raw := agent.Annotations[dependencyCircuitUntil]
	if raw == "" {
		return false, time.Time{}, ""
	}
	until, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false, time.Time{}, ""
	}
	if time.Now().UTC().After(until) {
		return false, time.Time{}, ""
	}
	return true, until, firstNonEmptyString(agent.Annotations[dependencyCircuitDepKey], "unknown")
}

func clearDependencyFailureState(agent *agscalev1alpha1.Agent) bool {
	if len(agent.Annotations) == 0 {
		return false
	}
	changed := false
	for _, key := range []string{
		dependencyFailurePrefix + "llm",
		dependencyFailurePrefix + "memory",
		dependencyFailurePrefix + "context",
		dependencyFailurePrefix + "skill",
		dependencyFailurePrefix + "unknown",
		dependencyCircuitDepKey,
		dependencyCircuitUntil,
	} {
		if _, ok := agent.Annotations[key]; ok {
			delete(agent.Annotations, key)
			changed = true
		}
	}
	return changed
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
		policy.Spec.Remediation.Actions = []string{"RestartPods", "ScaleOut", "SwitchToBackupLLM", "RollbackRevision", "FailOpenReadOnly"}
	}
}

func (r *AgentReconciler) resolveReferences(ctx context.Context, agent *agscalev1alpha1.Agent, toolPolicy *agscalev1alpha1.AgentToolPolicy) ([]corev1.EnvVar, error) {
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
		if llm.Spec.Auth.SecretRef.Name != "" {
			key := llm.Spec.Auth.SecretRef.Key
			if key == "" {
				key = "apiKey"
			}
			env = append(env, corev1.EnvVar{
				Name: "AGSCALE_LLM_API_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: llm.Spec.Auth.SecretRef.Name},
						Key:                  key,
					},
				},
			})
		}
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
		if err := enforceToolPolicyOnSkill(agent, toolPolicy, &skill); err != nil {
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

func (r *AgentReconciler) resolveToolPolicy(ctx context.Context, agent *agscalev1alpha1.Agent) (*agscalev1alpha1.AgentToolPolicy, error) {
	var list agscalev1alpha1.AgentToolPolicyList
	if err := r.List(ctx, &list, client.InNamespace(agent.Namespace)); err != nil {
		return nil, err
	}
	var matches []*agscalev1alpha1.AgentToolPolicy
	for i := range list.Items {
		p := &list.Items[i]
		if toolPolicyAppliesToAgent(p, agent) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		for _, p := range matches {
			names = append(names, p.Name)
		}
		return nil, fmt.Errorf("multiple AgentToolPolicies match agent %q: %s", agent.Name, strings.Join(names, ","))
	}
	return matches[0], nil
}

func enforceToolPolicyOnSkill(agent *agscalev1alpha1.Agent, policy *agscalev1alpha1.AgentToolPolicy, skill *agscalev1alpha1.AgentSkill) error {
	if policy == nil {
		return nil
	}
	if policy.Spec.ApprovalMode == "HumanGate" && agent.Annotations["agscale.io/tool-approval"] != "approved" {
		return fmt.Errorf("tool policy %q requires human approval (set annotation agscale.io/tool-approval=approved)", policy.Name)
	}
	if len(policy.Spec.AllowedSkillRefs) > 0 {
		allowed := false
		for _, ref := range policy.Spec.AllowedSkillRefs {
			if ref.Name == skill.Name {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("tool policy %q denies skill %q", policy.Name, skill.Name)
		}
	}
	if len(policy.Spec.AllowedProtocols) > 0 && skill.Spec.Source != nil && skill.Spec.Source.Protocol != "" {
		allowed := false
		for _, p := range policy.Spec.AllowedProtocols {
			if p == skill.Spec.Source.Protocol {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("tool policy %q denies protocol %q for skill %q", policy.Name, skill.Spec.Source.Protocol, skill.Name)
		}
	}
	return nil
}

func toolPolicyAppliesToAgent(policy *agscalev1alpha1.AgentToolPolicy, agent *agscalev1alpha1.Agent) bool {
	if policy == nil {
		return false
	}
	selector := policy.Spec.AgentSelector
	if len(selector.Names) == 0 && len(selector.MatchLabels) == 0 {
		return false
	}
	if len(selector.Names) > 0 {
		nameMatch := false
		for _, n := range selector.Names {
			if n == agent.Name {
				nameMatch = true
				break
			}
		}
		if !nameMatch {
			return false
		}
	}
	if len(selector.MatchLabels) > 0 {
		labels := agent.GetLabels()
		for k, v := range selector.MatchLabels {
			if labels[k] != v {
				return false
			}
		}
	}
	return true
}

func (r *AgentReconciler) recordToolPolicyViolation(ctx context.Context, policy *agscalev1alpha1.AgentToolPolicy, message string) error {
	if policy == nil {
		return nil
	}
	current := &agscalev1alpha1.AgentToolPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name}, current); err != nil {
		return err
	}
	now := metav1.Now()
	current.Status.ObservedGeneration = current.Generation
	current.Status.Enforced = true
	current.Status.Violations = current.Status.Violations + 1
	current.Status.LastViolationTime = &now
	meta.SetStatusCondition(&current.Status.Conditions, metav1.Condition{
		Type:               "Enforced",
		Status:             metav1.ConditionTrue,
		Reason:             "ViolationRecorded",
		Message:            message,
		LastTransitionTime: now,
		ObservedGeneration: current.Generation,
	})
	return r.Status().Update(ctx, current)
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
	volumes := mergeVolumes(runtimeObj.Spec.Volumes, agent.Spec.Volumes)
	volumeMounts := mergeVolumeMounts(runtimeObj.Spec.VolumeMounts, agent.Spec.VolumeMounts)

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
							VolumeMounts:    volumeMounts,
							Resources:       resources,
							LivenessProbe:   runtimeObj.Spec.LivenessProbe,
							ReadinessProbe:  runtimeObj.Spec.ReadinessProbe,
							SecurityContext: runtimeObj.Spec.SecurityContext,
						},
					},
					Volumes: volumes,
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

func mergeVolumes(base []corev1.Volume, override []corev1.Volume) []corev1.Volume {
	out := make([]corev1.Volume, 0, len(base)+len(override))
	index := map[string]int{}
	for _, v := range base {
		if v.Name == "" {
			continue
		}
		index[v.Name] = len(out)
		out = append(out, v)
	}
	for _, v := range override {
		if v.Name == "" {
			continue
		}
		if i, ok := index[v.Name]; ok {
			out[i] = v
			continue
		}
		index[v.Name] = len(out)
		out = append(out, v)
	}
	return out
}

func mergeVolumeMounts(base []corev1.VolumeMount, override []corev1.VolumeMount) []corev1.VolumeMount {
	out := make([]corev1.VolumeMount, 0, len(base)+len(override))
	index := map[string]int{}
	for _, m := range base {
		if m.Name == "" {
			continue
		}
		index[m.Name] = len(out)
		out = append(out, m)
	}
	for _, m := range override {
		if m.Name == "" {
			continue
		}
		if i, ok := index[m.Name]; ok {
			out[i] = m
			continue
		}
		index[m.Name] = len(out)
		out = append(out, m)
	}
	return out
}

func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agscalev1alpha1.Agent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Watches(&agscalev1alpha1.AgentToolPolicy{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			var agents agscalev1alpha1.AgentList
			if err := r.List(ctx, &agents, client.InNamespace(obj.GetNamespace())); err != nil {
				return nil
			}
			reqs := make([]reconcile.Request, 0, len(agents.Items))
			for i := range agents.Items {
				reqs = append(reqs, reconcile.Request{
					NamespacedName: types.NamespacedName{Namespace: agents.Items[i].Namespace, Name: agents.Items[i].Name},
				})
			}
			return reqs
		})).
		Complete(r)
}
