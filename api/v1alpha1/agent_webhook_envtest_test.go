package v1alpha1

import (
	"context"
	"path/filepath"
	goruntime "runtime"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestAgentWebhookDefaultAndValidate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testEnv, k8sClient := setupWebhookEnvtest(t)
	defer func() {
		_ = testEnv.Stop()
	}()

	SetWebhookReader(k8sClient)
	t.Cleanup(func() { SetWebhookReader(nil) })

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "agscale-webhook-test"}}
	mustCreateWebhook(t, ctx, k8sClient, ns)

	runtimeObj := &AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "rt", Namespace: ns.Name},
		Spec: AgentRuntimeSpec{
			Image: "nginx:1.27",
		},
	}
	llm := &LLMEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "llm", Namespace: ns.Name},
		Spec: LLMEndpointSpec{
			Provider: "openai",
			Auth: LLMAuthSpec{
				SecretRef: SecretKeyReference{Name: "openai", Key: "apiKey"},
			},
		},
	}
	mem := &MemoryStore{
		ObjectMeta: metav1.ObjectMeta{Name: "mem", Namespace: ns.Name},
		Spec: MemoryStoreSpec{
			Type: "redis",
			Connection: MemoryConnectionSpec{
				SecretRef: &SecretKeyReference{Name: "redis-auth"},
			},
		},
	}
	cp := &ContextProvider{
		ObjectMeta: metav1.ObjectMeta{Name: "ctx", Namespace: ns.Name},
		Spec: ContextProviderSpec{
			Type: "git",
		},
	}
	skill := &AgentSkill{
		ObjectMeta: metav1.ObjectMeta{Name: "skill", Namespace: ns.Name},
		Spec: AgentSkillSpec{
			Type: "http-tool",
		},
	}
	policy := &AgentReliabilityPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default-reliability", Namespace: ns.Name},
		Spec: AgentReliabilityPolicySpec{
			Health: ReliabilityHealthSpec{MinReadyPercent: 80},
			Remediation: ReliabilityRemediationSpec{
				Actions:           []string{"RestartPods"},
				MaxActionsPerHour: 6,
				CooldownSeconds:   300,
			},
		},
	}
	mustCreateWebhook(t, ctx, k8sClient, runtimeObj, llm, mem, cp, skill, policy)

	agent := &Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: ns.Name},
		Spec: AgentSpec{
			RuntimeRef:           LocalObjectReference{Name: runtimeObj.Name},
			ReliabilityPolicyRef: &LocalObjectReference{Name: policy.Name},
			LLMEndpointRef:       &LocalObjectReference{Name: llm.Name},
			MemoryRefs:           []LocalObjectReference{{Name: mem.Name}},
			ContextRefs:          []LocalObjectReference{{Name: cp.Name}},
			SkillRefs:            []LocalObjectReference{{Name: skill.Name}},
			Service: AgentServiceSpec{
				Enabled: true,
			},
		},
	}

	agent.Default()
	if agent.Spec.Replicas == nil || *agent.Spec.Replicas != 1 {
		t.Fatalf("default replicas not applied: %+v", agent.Spec.Replicas)
	}
	if agent.Spec.Service.Port != 80 || agent.Spec.Service.TargetPort != 8080 {
		t.Fatalf("default service ports not applied: port=%d target=%d", agent.Spec.Service.Port, agent.Spec.Service.TargetPort)
	}
	if _, err := agent.ValidateCreate(); err != nil {
		t.Fatalf("expected valid agent after defaulting, got error: %v", err)
	}

	bad := agent.DeepCopy()
	bad.Spec.RuntimeRef.Name = "missing-runtime"
	if _, err := bad.ValidateCreate(); err == nil {
		t.Fatalf("expected runtime reference validation error")
	}
}

func setupWebhookEnvtest(t *testing.T) (*envtest.Environment, client.Client) {
	t.Helper()

	_, thisFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatalf("unable to resolve current file path")
	}
	crdPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "config", "crd", "bases")

	scheme := k8sruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(AddToScheme(scheme))

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
	return testEnv, k8sClient
}

func mustCreateWebhook(t *testing.T, ctx context.Context, c client.Client, objs ...client.Object) {
	t.Helper()
	for _, obj := range objs {
		if err := c.Create(ctx, obj); err != nil {
			t.Fatalf("failed creating %T: %v", obj, err)
		}
	}
}
