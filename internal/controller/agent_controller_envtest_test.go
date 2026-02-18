package controller

import (
	"context"
	"path/filepath"
	goruntime "runtime"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	agscalev1alpha1 "agscale/api/v1alpha1"
)

func TestAgentReconcileCreatesWorkloads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testEnv, k8sClient, scheme := setupControllerEnvtest(t)
	defer func() {
		_ = testEnv.Stop()
	}()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "agscale-test"}}
	mustCreate(t, ctx, k8sClient, ns)

	runtimeObj := &agscalev1alpha1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "rt", Namespace: ns.Name},
		Spec: agscalev1alpha1.AgentRuntimeSpec{
			Image: "nginx:1.27",
		},
	}
	llm := &agscalev1alpha1.LLMEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "llm", Namespace: ns.Name},
		Spec: agscalev1alpha1.LLMEndpointSpec{
			Provider: "openai",
			BaseURL:  "https://api.openai.com/v1",
			Auth: agscalev1alpha1.LLMAuthSpec{
				SecretRef: agscalev1alpha1.SecretKeyReference{Name: "openai", Key: "apiKey"},
			},
		},
	}
	mem := &agscalev1alpha1.MemoryStore{
		ObjectMeta: metav1.ObjectMeta{Name: "mem", Namespace: ns.Name},
		Spec: agscalev1alpha1.MemoryStoreSpec{
			Type: "redis",
			Connection: agscalev1alpha1.MemoryConnectionSpec{
				Endpoint:  "redis.default.svc:6379",
				SecretRef: &agscalev1alpha1.SecretKeyReference{Name: "redis-auth"},
			},
		},
	}
	cp := &agscalev1alpha1.ContextProvider{
		ObjectMeta: metav1.ObjectMeta{Name: "ctx", Namespace: ns.Name},
		Spec: agscalev1alpha1.ContextProviderSpec{
			Type: "git",
		},
	}
	skill := &agscalev1alpha1.AgentSkill{
		ObjectMeta: metav1.ObjectMeta{Name: "skill", Namespace: ns.Name},
		Spec: agscalev1alpha1.AgentSkillSpec{
			Type: "http-tool",
		},
	}
	policy := &agscalev1alpha1.AgentReliabilityPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default-reliability", Namespace: ns.Name},
		Spec: agscalev1alpha1.AgentReliabilityPolicySpec{
			Health: agscalev1alpha1.ReliabilityHealthSpec{MinReadyPercent: 80},
			Remediation: agscalev1alpha1.ReliabilityRemediationSpec{
				Actions:           []string{"RestartPods"},
				MaxActionsPerHour: 6,
				CooldownSeconds:   300,
			},
		},
	}

	mustCreate(t, ctx, k8sClient, runtimeObj, llm, mem, cp, skill, policy)

	agent := &agscalev1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: ns.Name},
		Spec: agscalev1alpha1.AgentSpec{
			RuntimeRef:           agscalev1alpha1.LocalObjectReference{Name: runtimeObj.Name},
			ReliabilityPolicyRef: &agscalev1alpha1.LocalObjectReference{Name: policy.Name},
			LLMEndpointRef:       &agscalev1alpha1.LocalObjectReference{Name: llm.Name},
			MemoryRefs:           []agscalev1alpha1.LocalObjectReference{{Name: mem.Name}},
			ContextRefs:          []agscalev1alpha1.LocalObjectReference{{Name: cp.Name}},
			SkillRefs:            []agscalev1alpha1.LocalObjectReference{{Name: skill.Name}},
			Service: agscalev1alpha1.AgentServiceSpec{
				Enabled:    true,
				Port:       80,
				TargetPort: 8080,
				Type:       corev1.ServiceTypeClusterIP,
			},
			Autoscaling: &agscalev1alpha1.AgentAutoscalingSpec{
				MinReplicas:                    1,
				MaxReplicas:                    3,
				TargetCPUUtilizationPercentage: 70,
			},
		},
	}
	mustCreate(t, ctx, k8sClient, agent)

	reconciler := &AgentReconciler{Client: k8sClient, Scheme: scheme}
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var deployFound appsv1.Deployment
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, &deployFound); err != nil {
		t.Fatalf("deployment not found: %v", err)
	}
	if len(deployFound.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected single container, got %d", len(deployFound.Spec.Template.Spec.Containers))
	}
	var foundLLMAPIKey bool
	for _, e := range deployFound.Spec.Template.Spec.Containers[0].Env {
		if e.Name != "AGSCALE_LLM_API_KEY" {
			continue
		}
		if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
			t.Fatalf("AGSCALE_LLM_API_KEY must use secretKeyRef")
		}
		if e.ValueFrom.SecretKeyRef.Name != "openai" || e.ValueFrom.SecretKeyRef.Key != "apiKey" {
			t.Fatalf("unexpected AGSCALE_LLM_API_KEY secret reference: %+v", e.ValueFrom.SecretKeyRef)
		}
		foundLLMAPIKey = true
	}
	if !foundLLMAPIKey {
		t.Fatalf("expected AGSCALE_LLM_API_KEY env var to be injected")
	}

	var serviceFound corev1.Service
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, &serviceFound); err != nil {
		t.Fatalf("service not found: %v", err)
	}

	var hpaFound autoscalingv2.HorizontalPodAutoscaler
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, &hpaFound); err != nil {
		t.Fatalf("hpa not found: %v", err)
	}
}

func setupControllerEnvtest(t *testing.T) (*envtest.Environment, client.Client, *k8sruntime.Scheme) {
	t.Helper()

	_, thisFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatalf("unable to resolve current file path")
	}
	crdPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "config", "crd", "bases")

	scheme := k8sruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(agscalev1alpha1.AddToScheme(scheme))

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{crdPath},
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Skipf("envtest not available in this environment: %v", err)
	}

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		_ = testEnv.Stop()
		t.Fatalf("failed to create k8s client: %v", err)
	}
	return testEnv, k8sClient, scheme
}

func mustCreate(t *testing.T, ctx context.Context, c client.Client, objs ...client.Object) {
	t.Helper()
	for _, obj := range objs {
		if err := c.Create(ctx, obj); err != nil {
			t.Fatalf("failed creating %T: %v", obj, err)
		}
	}
}
