package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	agscalev1alpha1 "agscale/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultNamespace = "default"

func Execute(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "version":
		fmt.Println("agscale cli v0.2.0")
		return nil
	case "opencode":
		return runOpenCode(args[1:])
	case "cluster":
		return runCluster(args[1:])
	case "get":
		warnDeprecated("agscale get", "agscale cluster get")
		return runGet(args[1:])
	case "describe":
		warnDeprecated("agscale describe", "agscale cluster describe")
		return runDescribe(args[1:])
	case "logs":
		warnDeprecated("agscale logs", "agscale cluster logs")
		return runLogs(args[1:])
	case "apply":
		warnDeprecated("agscale apply", "agscale cluster apply")
		return runApply(args[1:])
	case "doctor":
		warnDeprecated("agscale doctor", "agscale cluster doctor")
		return runDoctor(args[1:])
	case "team":
		return runTeam(args[1:])
	case "fleet":
		return runFleet(args[1:])
	case "tui":
		return runTUI(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage() {
	fmt.Print(`agscale CLI

Usage:
  agscale <command> [flags]

Commands:
  version                          Print CLI version
  opencode template                Print BYO OpenCode Secret + LLMEndpoint templates
  cluster doctor                   Validate cluster and CRDs availability
  cluster get <agents|workflows|runs|skills> [-n namespace]
  cluster describe agent <name> [-n namespace]
  cluster logs agent <name> [-n namespace] [--tail N]
  cluster apply -f <file> | -k <kustomize-dir> [-n namespace]
  cluster tui agent <name> [-n namespace]
  team init <team-name> [-n namespace] [--profile openclaw|openhands|openai] [--out file] [--apply]
  team up <team-name> [-n namespace] [--profile openclaw|openhands|openai] [--out file]
  team validate -f <bundle.yaml> [-n namespace]
  team watch <run-name> [-n namespace] [--interval-seconds 2] [--timeout-seconds 0]
  team run <workflow> [-n namespace] [--trigger manual] [--correlation-id id]
  team status <run-name> [-n namespace]
  team graph <workflow> [-n namespace]
  team command-center <task-name> [-n namespace] [--selector k=v] [--agents a,b] [--exclude-agents x,y] [--require-skills s1,s2] [--max-agents 3] [--topology dag|sequential] [--entry-agent name] [--protocol http|mcp|a2a] [--out file] [--apply] [--run] [--explain-selection]
  fleet scan [--local] [--hosts host1,host2] [--scan-mode agent-aware|ports] [--signatures openclaw,opencode,...] [--ssh-config ~/.ssh/config] [--ports 18789,8080] [--write]
  fleet list [--status healthy|degraded|unknown|all] [--protocol http|mcp|all]
  fleet inspect <agent-id-or-name>
  fleet run <task-name> [--agents a,b] [--selector k=v] [--require-skills s1,s2] [--max-agents 3] [--topology dag|sequential] [--dry-run]
  tui [<agent-id-or-name>] [--namespace default] [--inventory path]

Deprecated aliases:
  doctor|get|describe|logs|apply
  tui agent <name>                Use 'tui <name>'
`)
}

func runOpenCode(args []string) error {
	if len(args) == 0 || args[0] != "template" {
		return errors.New("usage: opencode template [--namespace default --name shared-opencode --secret opencode-api-key --base-url <url> --model <model>]")
	}
	fs := flag.NewFlagSet("opencode template", flag.ContinueOnError)
	namespace := fs.String("namespace", defaultNamespace, "namespace")
	name := fs.String("name", "shared-opencode", "llmendpoint name")
	secret := fs.String("secret", "opencode-api-key", "secret name")
	secretKey := fs.String("secret-key", "apiKey", "secret key field")
	baseURL := fs.String("base-url", "https://your-opencode-endpoint.example/v1", "OpenCode API base URL")
	model := fs.String("model", "your-opencode-model", "default model")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	fmt.Printf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: Opaque
stringData:
  %s: "<YOUR_OPENCODE_API_KEY>"
---
apiVersion: agscale.io/v1alpha1
kind: LLMEndpoint
metadata:
  name: %s
  namespace: %s
spec:
  provider: opencode
  baseURL: %s
  defaultModel: %s
  auth:
    secretRef:
      name: %s
      key: %s
  timeoutSeconds: 30
`, *secret, *namespace, *secretKey, *name, *namespace, *baseURL, *model, *secret, *secretKey)
	return nil
}

func runGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("get requires resource type")
	}
	ctx := context.Background()
	client, _, err := kubeClients()
	if err != nil {
		return err
	}

	switch rest[0] {
	case "agents":
		var list agscalev1alpha1.AgentList
		if err := client.List(ctx, &list, ctrlclient.InNamespace(*namespace)); err != nil {
			return err
		}
		sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tREADY\tBUDGET\tURL")
		for _, a := range list.Items {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", a.Name, a.Status.ReadyReplicas, a.Status.BudgetPhase, a.Status.URL)
		}
		return tw.Flush()
	case "workflows":
		var list agscalev1alpha1.AgentWorkflowList
		if err := client.List(ctx, &list, ctrlclient.InNamespace(*namespace)); err != nil {
			return err
		}
		sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tMODE\tPHASE\tSTEPS")
		for _, w := range list.Items {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", w.Name, w.Spec.Mode, w.Status.Phase, len(w.Spec.Steps))
		}
		return tw.Flush()
	case "runs":
		var list agscalev1alpha1.AgentWorkflowRunList
		if err := client.List(ctx, &list, ctrlclient.InNamespace(*namespace)); err != nil {
			return err
		}
		sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tWORKFLOW\tPHASE\tSTARTED")
		for _, r := range list.Items {
			started := ""
			if r.Status.StartedAt != nil {
				started = r.Status.StartedAt.Time.Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, r.Spec.WorkflowRef.Name, r.Status.Phase, started)
		}
		return tw.Flush()
	case "skills":
		var list agscalev1alpha1.AgentSkillList
		if err := client.List(ctx, &list, ctrlclient.InNamespace(*namespace)); err != nil {
			return err
		}
		sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tTYPE\tRESOLVED\tENDPOINT")
		for _, s := range list.Items {
			fmt.Fprintf(tw, "%s\t%s\t%t\t%s\n", s.Name, s.Spec.Type, s.Status.SourceResolved, s.Spec.Endpoint)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("unsupported resource %q", rest[0])
	}
}

func runDescribe(args []string) error {
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 {
		return errors.New("usage: describe agent <name>")
	}
	if rest[0] != "agent" {
		return fmt.Errorf("unsupported describe target %q", rest[0])
	}
	client, _, err := kubeClients()
	if err != nil {
		return err
	}
	ctx := context.Background()
	agent := &agscalev1alpha1.Agent{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: *namespace, Name: rest[1]}, agent); err != nil {
		return err
	}
	fmt.Printf("Name: %s\nNamespace: %s\n", agent.Name, agent.Namespace)
	fmt.Printf("Runtime: %s\n", agent.Spec.RuntimeRef.Name)
	if agent.Spec.LLMEndpointRef != nil {
		fmt.Printf("LLM Endpoint: %s\n", agent.Spec.LLMEndpointRef.Name)
	}
	fmt.Printf("Replicas: %d ready\n", agent.Status.ReadyReplicas)
	fmt.Printf("Service URL: %s\n", agent.Status.URL)
	fmt.Printf("Budget Phase: %s\n", agent.Status.BudgetPhase)
	fmt.Println("Conditions:")
	for _, c := range agent.Status.Conditions {
		fmt.Printf("- %s=%s (%s): %s\n", c.Type, c.Status, c.Reason, c.Message)
	}
	return nil
}

func runLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	tail := fs.Int64("tail", 100, "tail lines")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 || rest[0] != "agent" {
		return errors.New("usage: logs agent <name>")
	}
	_, kc, err := kubeClients()
	if err != nil {
		return err
	}
	ctx := context.Background()
	pod, err := firstAgentPod(ctx, kc, *namespace, rest[1])
	if err != nil {
		return err
	}
	return streamPodLogs(ctx, kc, *namespace, pod.Name, *tail, os.Stdout)
}

func runApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	file := fs.String("f", "", "file")
	kustomizeDir := fs.String("k", "", "kustomize dir")
	namespace := fs.String("n", "", "namespace override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" && *kustomizeDir == "" {
		return errors.New("usage: apply -f <file> | -k <dir>")
	}
	cmdArgs := []string{"apply"}
	if *file != "" {
		cmdArgs = append(cmdArgs, "-f", *file)
	}
	if *kustomizeDir != "" {
		cmdArgs = append(cmdArgs, "-k", *kustomizeDir)
	}
	if *namespace != "" {
		cmdArgs = append(cmdArgs, "-n", *namespace)
	}
	cmd := exec.Command("kubectl", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, kc, err := kubeClients()
	if err != nil {
		return err
	}
	ctx := context.Background()
	fmt.Println("agscale doctor")
	fmt.Println("-------------")
	if _, err := kc.CoreV1().Namespaces().Get(ctx, *namespace, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("namespace check failed: %w", err)
	}
	fmt.Printf("[ok] namespace %q reachable\n", *namespace)

	checks := []struct {
		name string
		obj  ctrlclient.ObjectList
	}{
		{name: "Agent", obj: &agscalev1alpha1.AgentList{}},
		{name: "AgentWorkflow", obj: &agscalev1alpha1.AgentWorkflowList{}},
		{name: "AgentWorkflowRun", obj: &agscalev1alpha1.AgentWorkflowRunList{}},
		{name: "AgentSkill", obj: &agscalev1alpha1.AgentSkillList{}},
	}
	for _, check := range checks {
		if err := client.List(ctx, check.obj, ctrlclient.InNamespace(*namespace), ctrlclient.Limit(1)); err != nil {
			return fmt.Errorf("resource check failed for %s: %w", check.name, err)
		}
		fmt.Printf("[ok] %s CRD/list works\n", check.name)
	}
	fmt.Println("[ok] cluster connectivity and core agscale CRDs healthy")
	return nil
}

func runTeam(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: team <init|up|validate|watch|run|status|graph|command-center>")
	}
	switch args[0] {
	case "init":
		return runTeamInit(args[1:])
	case "up":
		return runTeamUp(args[1:])
	case "validate":
		return runTeamValidate(args[1:])
	case "watch":
		return runTeamWatch(args[1:])
	case "run":
		return runTeamRun(args[1:])
	case "status":
		return runTeamStatus(args[1:])
	case "graph":
		return runTeamGraph(args[1:])
	case "command-center":
		return runTeamCommandCenter(args[1:])
	default:
		return fmt.Errorf("unknown team subcommand %q", args[0])
	}
}

func runTeamRun(args []string) error {
	fs := flag.NewFlagSet("team run", flag.ContinueOnError)
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	trigger := fs.String("trigger", "manual", "trigger")
	correlationID := fs.String("correlation-id", "", "correlation id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: team run <workflow>")
	}
	client, _, err := kubeClients()
	if err != nil {
		return err
	}
	ctx := context.Background()
	runName := fmt.Sprintf("%s-run-%d", rest[0], time.Now().Unix())
	run := &agscalev1alpha1.AgentWorkflowRun{
		ObjectMeta: metav1.ObjectMeta{Name: runName, Namespace: *namespace},
		Spec: agscalev1alpha1.AgentWorkflowRunSpec{
			WorkflowRef:   agscalev1alpha1.LocalObjectReference{Name: rest[0]},
			Trigger:       *trigger,
			CorrelationID: *correlationID,
		},
	}
	if err := client.Create(ctx, run); err != nil {
		return err
	}
	fmt.Printf("created AgentWorkflowRun %s/%s\n", run.Namespace, run.Name)
	return nil
}

func runTeamStatus(args []string) error {
	fs := flag.NewFlagSet("team status", flag.ContinueOnError)
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: team status <run-name>")
	}
	client, _, err := kubeClients()
	if err != nil {
		return err
	}
	ctx := context.Background()
	run := &agscalev1alpha1.AgentWorkflowRun{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: *namespace, Name: rest[0]}, run); err != nil {
		return err
	}
	fmt.Printf("Run: %s/%s\nWorkflow: %s\nPhase: %s\n", run.Namespace, run.Name, run.Spec.WorkflowRef.Name, run.Status.Phase)
	if run.Status.StartedAt != nil {
		fmt.Printf("Started: %s\n", run.Status.StartedAt.Time.Format(time.RFC3339))
	}
	if run.Status.CompletedAt != nil {
		fmt.Printf("Completed: %s\n", run.Status.CompletedAt.Time.Format(time.RFC3339))
	}
	if len(run.Status.StepResults) > 0 {
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "STEP\tSTATUS\tDURATION_MS\tERROR")
		for _, s := range run.Status.StepResults {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", s.StepName, s.Status, s.DurationMs, s.Error)
		}
		return tw.Flush()
	}
	return nil
}

func runTeamGraph(args []string) error {
	fs := flag.NewFlagSet("team graph", flag.ContinueOnError)
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: team graph <workflow>")
	}
	client, _, err := kubeClients()
	if err != nil {
		return err
	}
	ctx := context.Background()
	workflow := &agscalev1alpha1.AgentWorkflow{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: *namespace, Name: rest[0]}, workflow); err != nil {
		return err
	}
	fmt.Printf("Workflow %s/%s graph:\n", workflow.Namespace, workflow.Name)
	for i, step := range workflow.Spec.Steps {
		protocol := string(step.Protocol)
		if protocol == "" {
			if step.Type == agscalev1alpha1.WorkflowStepToolCall {
				protocol = "mcp"
			} else {
				protocol = "http"
			}
		}
		target := ""
		if step.AgentRef != nil {
			target = step.AgentRef.Name
		}
		if len(step.DependsOn) == 0 && i > 0 {
			fmt.Printf("- %s [%s] -> %s (dependsOn: %s)\n", step.Name, protocol, target, workflow.Spec.Steps[i-1].Name)
		} else if len(step.DependsOn) == 0 {
			fmt.Printf("- %s [%s] -> %s (root)\n", step.Name, protocol, target)
		} else {
			fmt.Printf("- %s [%s] -> %s (dependsOn: %s)\n", step.Name, protocol, target, strings.Join(step.DependsOn, ","))
		}
	}
	return nil
}

func runClusterTUI(args []string) error {
	namespace := defaultNamespace
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-n" || args[i] == "--namespace":
			if i+1 >= len(args) {
				return errors.New("usage: cluster tui agent <name> [-n namespace]")
			}
			namespace = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-n="):
			namespace = strings.TrimPrefix(args[i], "-n=")
		case strings.HasPrefix(args[i], "--namespace="):
			namespace = strings.TrimPrefix(args[i], "--namespace=")
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) < 2 || rest[0] != "agent" {
		return errors.New("usage: cluster tui agent <name>")
	}
	client, kc, err := kubeClients()
	if err != nil {
		return err
	}
	return runAgentTUIChatFirst(client, kc, namespace, rest[1])
}

func runAgentChat(reader *bufio.Reader, agent *agscalev1alpha1.Agent) error {
	if agent.Status.URL == "" {
		return errors.New("agent status url empty")
	}
	path := agent.Annotations["agscale.io/chat-path"]
	if path == "" {
		path = "/chat"
	}
	baseURL := strings.TrimSuffix(agent.Status.URL, "/")
	if override, ok := chatBaseURLFromEnv(agent.Name); ok {
		baseURL = strings.TrimSuffix(override, "/")
	}

	cleanup := func() {}
	if isClusterLocalURL(baseURL) {
		localURL, closeFn, err := startServicePortForward(agent.Namespace, agent.Name, servicePortFromURL(baseURL))
		if err != nil {
			return fmt.Errorf("agent URL is cluster-local and auto port-forward failed: %w", err)
		}
		baseURL = localURL
		cleanup = closeFn
	}
	defer cleanup()

	fmt.Print("prompt> ")
	prompt, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	body, err := buildChatRequestBody(agent.Name, strings.TrimSpace(prompt), path, "")
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token, ok := chatTokenFromEnv(agent.Name); ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("response (%d):\n%s\n", resp.StatusCode, strings.TrimSpace(string(out)))
	pause(reader)
	return nil
}

func chatTokenFromEnv(agentName string) (string, bool) {
	key := "AGSCALE_CHAT_TOKEN_" + strings.ToUpper(strings.ReplaceAll(agentName, "-", "_"))
	if v := os.Getenv(key); v != "" {
		return v, true
	}
	if v := os.Getenv("AGSCALE_CHAT_TOKEN"); v != "" {
		return v, true
	}
	return "", false
}

func chatBaseURLFromEnv(agentName string) (string, bool) {
	key := "AGSCALE_CHAT_BASE_URL_" + strings.ToUpper(strings.ReplaceAll(agentName, "-", "_"))
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v, true
	}
	if v := strings.TrimSpace(os.Getenv("AGSCALE_CHAT_BASE_URL")); v != "" {
		return v, true
	}
	return "", false
}

func chatModelFromEnv(agentName string) (string, bool) {
	key := "AGSCALE_OPENAI_MODEL_" + strings.ToUpper(strings.ReplaceAll(agentName, "-", "_"))
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v, true
	}
	if v := strings.TrimSpace(os.Getenv("AGSCALE_OPENAI_MODEL")); v != "" {
		return v, true
	}
	return "", false
}

func buildChatRequestBody(agentName, prompt, path, model string) ([]byte, error) {
	if isOpenAIChatPath(path) {
		resolvedModel := strings.TrimSpace(model)
		if resolvedModel == "" {
			if envModel, ok := chatModelFromEnv(agentName); ok {
				resolvedModel = envModel
			}
		}
		if resolvedModel == "" {
			return nil, fmt.Errorf("openai-compatible chat for %q requires model metadata or AGSCALE_OPENAI_MODEL", agentName)
		}
		return json.Marshal(map[string]any{
			"model": resolvedModel,
			"messages": []map[string]string{
				{
					"role":    "user",
					"content": prompt,
				},
			},
		})
	}
	return json.Marshal(map[string]string{"message": prompt})
}

func isClusterLocalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return strings.HasSuffix(host, ".svc.cluster.local") || strings.HasSuffix(host, ".svc")
}

func isOpenAIChatPath(path string) bool {
	return strings.HasSuffix(strings.TrimSuffix(path, "/"), "/v1/chat/completions")
}

func servicePortFromURL(raw string) int {
	u, err := url.Parse(raw)
	if err != nil {
		return 80
	}
	if port := strings.TrimSpace(u.Port()); port != "" {
		if n, err := strconv.Atoi(port); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	if strings.EqualFold(u.Scheme, "https") {
		return 443
	}
	return 80
}

func startServicePortForward(namespace, serviceName string, servicePort int) (string, func(), error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	if servicePort <= 0 {
		servicePort = 80
	}

	cmd := exec.Command("kubectl", "-n", namespace, "port-forward", "svc/"+serviceName, fmt.Sprintf("%d:%d", port, servicePort))
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}

	ready := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return "", nil, errors.New("timed out waiting for local port-forward")
	}

	cleanup := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), cleanup, nil
}

func pause(reader *bufio.Reader) {
	fmt.Print("\npress Enter to continue...")
	_, _ = reader.ReadString('\n')
}

func kubeClients() (ctrlclient.Client, *kubernetes.Clientset, error) {
	cfg, err := kubeConfig()
	if err != nil {
		return nil, nil, err
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}
	if err := agscalev1alpha1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}
	c, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, err
	}
	kc, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	return c, kc, nil
}

func kubeConfig() (*rest.Config, error) {
	if v := os.Getenv("KUBECONFIG"); v != "" {
		return clientcmd.BuildConfigFromFlags("", v)
	}
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfgOverrides := &clientcmd.ConfigOverrides{}
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, cfgOverrides)
	return clientCfg.ClientConfig()
}

func firstAgentPod(ctx context.Context, kc *kubernetes.Clientset, namespace, agentName string) (*corev1.Pod, error) {
	pods, err := kc.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + agentName})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found for agent %q", agentName)
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].CreationTimestamp.Time.After(pods.Items[j].CreationTimestamp.Time)
	})
	return &pods.Items[0], nil
}

func streamPodLogs(ctx context.Context, kc *kubernetes.Clientset, namespace, podName string, tail int64, out io.Writer) error {
	req := kc.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{TailLines: &tail})
	stream, err := req.Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()
	_, err = io.Copy(out, stream)
	return err
}

func restartAgentPod(ctx context.Context, kc *kubernetes.Clientset, namespace, agentName string) error {
	pod, err := firstAgentPod(ctx, kc, namespace, agentName)
	if err != nil {
		return err
	}
	return kc.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
}

func scaleAgent(ctx context.Context, c ctrlclient.Client, namespace, agentName string, replicas int32) error {
	agent := &agscalev1alpha1.Agent{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: agentName}, agent); err != nil {
		return err
	}
	agent.Spec.Replicas = &replicas
	return c.Update(ctx, agent)
}

func failoverAgentLLM(ctx context.Context, c ctrlclient.Client, namespace, agentName string) error {
	agent := &agscalev1alpha1.Agent{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: agentName}, agent); err != nil {
		return err
	}
	backup := agent.Annotations["agscale.io/backup-llm-endpoint"]
	if backup == "" {
		return fmt.Errorf("missing annotation agscale.io/backup-llm-endpoint")
	}
	agent.Spec.LLMEndpointRef = &agscalev1alpha1.LocalObjectReference{Name: backup}
	return c.Update(ctx, agent)
}
