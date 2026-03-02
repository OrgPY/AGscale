package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	agscalev1alpha1 "agscale/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type teamBuilderStep struct {
	Name         string
	Agent        string
	Type         string
	Protocol     string
	EndpointPath string
	TimeoutSec   int
	DependsOn    []string
}

type teamBuilderSpec struct {
	TeamName         string
	Namespace        string
	Profile          string
	EntryAgent       string
	Steps            []teamBuilderStep
	ToolPolicy       string
	BudgetPolicy     string
	DataPolicy       string
	EnableEvent      bool
	IntegrationName  string
	IntegrationURL   string
	EventTopic       string
	EventIntervalSec int
	CreateRun        bool
	Apply            bool
	OutFile          string
}

func runTeamInit(args []string) error {
	return runTeamCreate(args, false)
}

func runTeamUp(args []string) error {
	return runTeamCreate(args, true)
}

func runTeamCreate(args []string, forceApplyAndRun bool) error {
	fs := newTeamCreateFlagSet("team init")
	if forceApplyAndRun {
		fs = newTeamCreateFlagSet("team up")
	}
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	profile := fs.String("profile", "openclaw", "profile: openclaw|openhands|openai")
	outFile := fs.String("out", "", "output bundle file")
	applyNow := fs.Bool("apply", false, "apply generated bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		if forceApplyAndRun {
			return errors.New("usage: team up <team-name> [-n namespace] [--profile ...] [--out file]")
		}
		return errors.New("usage: team init <team-name> [-n namespace] [--profile ...] [--out file] [--apply]")
	}
	p := strings.ToLower(strings.TrimSpace(*profile))
	if p != "openclaw" && p != "openhands" && p != "openai" {
		return errors.New("invalid profile; supported: openclaw|openhands|openai")
	}

	spec := teamBuilderSpec{
		TeamName:         rest[0],
		Namespace:        *namespace,
		Profile:          p,
		EventIntervalSec: 120,
	}
	if *outFile != "" {
		spec.OutFile = *outFile
	} else {
		spec.OutFile = fmt.Sprintf("%s-team.yaml", spec.TeamName)
	}

	reader := bufio.NewReader(os.Stdin)
	if err := fillTeamBuilderSpec(reader, &spec, forceApplyAndRun, *applyNow); err != nil {
		return err
	}
	if err := validateTeamBuilderSpec(spec); err != nil {
		return err
	}

	bundle := renderTeamBundleYAML(spec)
	if err := os.WriteFile(spec.OutFile, []byte(bundle), 0644); err != nil {
		return err
	}
	fmt.Printf("generated team bundle: %s\n", spec.OutFile)

	if spec.Apply {
		if err := runKubectlApply(spec.Namespace, spec.OutFile); err != nil {
			return err
		}
		fmt.Printf("applied bundle to namespace %q\n", spec.Namespace)
		if spec.CreateRun {
			runName := fmt.Sprintf("%s-run-%d", spec.TeamName, time.Now().Unix())
			if err := createWorkflowRun(spec.Namespace, spec.TeamName, runName); err != nil {
				return err
			}
			fmt.Printf("created AgentWorkflowRun %s/%s\n", spec.Namespace, runName)
			fmt.Printf("watch it with: ./bin/agscale team watch %s -n %s\n", runName, spec.Namespace)
		}
	}
	return nil
}

func newTeamCreateFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}

func fillTeamBuilderSpec(reader *bufio.Reader, spec *teamBuilderSpec, forceApplyAndRun, applyFlag bool) error {
	fmt.Printf("team builder wizard (%s profile)\n", spec.Profile)
	fmt.Printf("team name: %s\nnamespace: %s\n\n", spec.TeamName, spec.Namespace)

	entry, err := promptString(reader, "entry agent name", "support-agent")
	if err != nil {
		return err
	}
	spec.EntryAgent = entry

	stepCount, err := promptInt(reader, "number of workflow steps", 3, 1, 20)
	if err != nil {
		return err
	}
	spec.Steps = make([]teamBuilderStep, 0, stepCount)
	defaultTimeout, defaultPath := profileDefaults(spec.Profile)
	for i := 0; i < stepCount; i++ {
		fmt.Printf("\nstep %d/%d\n", i+1, stepCount)
		defaultName := fmt.Sprintf("step-%d", i+1)
		if i == 0 {
			defaultName = "triage"
		} else if i == 1 {
			defaultName = "retrieval"
		} else if i == 2 {
			defaultName = "response"
		}
		name, err := promptString(reader, "step name", defaultName)
		if err != nil {
			return err
		}
		agent, err := promptString(reader, "target agent", spec.EntryAgent)
		if err != nil {
			return err
		}
		stepType, err := promptString(reader, "step type (Prompt|Retrieve|ToolCall|Transform|Route)", defaultStepType(i))
		if err != nil {
			return err
		}
		protocol, err := promptString(reader, "protocol (http|mcp)", "http")
		if err != nil {
			return err
		}
		timeoutSec, err := promptInt(reader, "timeout seconds", defaultTimeout, 1, 600)
		if err != nil {
			return err
		}
		endpointPath, err := promptString(reader, "endpoint path (blank uses controller default)", defaultPath)
		if err != nil {
			return err
		}

		dependsOn := []string{}
		if i > 0 {
			prev := spec.Steps[i-1].Name
			dep, err := promptString(reader, "dependsOn (comma-separated step names)", prev)
			if err != nil {
				return err
			}
			dependsOn = splitCSV(dep)
		}
		spec.Steps = append(spec.Steps, teamBuilderStep{
			Name:         strings.TrimSpace(name),
			Agent:        strings.TrimSpace(agent),
			Type:         normalizeStepType(stepType),
			Protocol:     strings.ToLower(strings.TrimSpace(protocol)),
			EndpointPath: strings.TrimSpace(endpointPath),
			TimeoutSec:   timeoutSec,
			DependsOn:    dependsOn,
		})
	}

	if v, err := promptString(reader, "tool policy ref (optional)", ""); err != nil {
		return err
	} else {
		spec.ToolPolicy = strings.TrimSpace(v)
	}
	if v, err := promptString(reader, "budget policy ref (optional)", ""); err != nil {
		return err
	} else {
		spec.BudgetPolicy = strings.TrimSpace(v)
	}
	if v, err := promptString(reader, "data governance policy ref (optional)", ""); err != nil {
		return err
	} else {
		spec.DataPolicy = strings.TrimSpace(v)
	}

	enableEvent, err := promptBool(reader, "enable EventSource + IntegrationEndpoint?", false)
	if err != nil {
		return err
	}
	spec.EnableEvent = enableEvent
	if spec.EnableEvent {
		v, err := promptString(reader, "integration endpoint name", spec.TeamName+"-integration")
		if err != nil {
			return err
		}
		spec.IntegrationName = v
		v, err = promptString(reader, "integration health URL", "https://example.com/health")
		if err != nil {
			return err
		}
		spec.IntegrationURL = v
		v, err = promptString(reader, "event topic", spec.TeamName+".event")
		if err != nil {
			return err
		}
		spec.EventTopic = v
		iv, err := promptInt(reader, "event interval seconds", spec.EventIntervalSec, 10, 86400)
		if err != nil {
			return err
		}
		spec.EventIntervalSec = iv
	}

	if forceApplyAndRun {
		spec.CreateRun = true
		spec.Apply = true
		return nil
	}

	createRun, err := promptBool(reader, "create first workflow run after apply?", true)
	if err != nil {
		return err
	}
	spec.CreateRun = createRun
	if applyFlag {
		spec.Apply = true
		return nil
	}
	applyNowPrompt, err := promptBool(reader, "apply bundle now with kubectl?", false)
	if err != nil {
		return err
	}
	spec.Apply = applyNowPrompt
	return nil
}

func profileDefaults(profile string) (timeout int, endpointPath string) {
	switch profile {
	case "openai":
		return 20, "/chat"
	case "openhands":
		return 30, "/invoke"
	default:
		return 20, ""
	}
}

func defaultStepType(i int) string {
	switch i {
	case 0:
		return "Prompt"
	case 1:
		return "Retrieve"
	default:
		return "Transform"
	}
}

func normalizeStepType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "prompt":
		return "Prompt"
	case "retrieve":
		return "Retrieve"
	case "toolcall":
		return "ToolCall"
	case "transform":
		return "Transform"
	case "route":
		return "Route"
	default:
		return "Prompt"
	}
}

func validateTeamBuilderSpec(spec teamBuilderSpec) error {
	if spec.TeamName == "" {
		return errors.New("team name must not be empty")
	}
	if spec.Namespace == "" {
		return errors.New("namespace must not be empty")
	}
	if spec.EntryAgent == "" {
		return errors.New("entry agent must not be empty")
	}
	if len(spec.Steps) == 0 {
		return errors.New("at least one step is required")
	}
	known := map[string]struct{}{}
	for _, s := range spec.Steps {
		if s.Name == "" {
			return errors.New("step name must not be empty")
		}
		if _, exists := known[s.Name]; exists {
			return fmt.Errorf("duplicate step name %q", s.Name)
		}
		known[s.Name] = struct{}{}
		if s.Agent == "" {
			return fmt.Errorf("step %q must reference an agent", s.Name)
		}
		if s.Protocol != "http" && s.Protocol != "mcp" {
			return fmt.Errorf("step %q has unsupported protocol %q", s.Name, s.Protocol)
		}
	}
	for _, s := range spec.Steps {
		for _, dep := range s.DependsOn {
			if _, exists := known[dep]; !exists {
				return fmt.Errorf("step %q depends on unknown step %q", s.Name, dep)
			}
		}
	}
	if hasStepCycle(spec.Steps) {
		return errors.New("workflow step dependencies contain a cycle")
	}
	return nil
}

func hasStepCycle(steps []teamBuilderStep) bool {
	graph := map[string][]string{}
	for _, s := range steps {
		graph[s.Name] = append([]string{}, s.DependsOn...)
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var dfs func(string) bool
	dfs = func(n string) bool {
		if visiting[n] {
			return true
		}
		if visited[n] {
			return false
		}
		visiting[n] = true
		for _, dep := range graph[n] {
			if dfs(dep) {
				return true
			}
		}
		visiting[n] = false
		visited[n] = true
		return false
	}
	for _, s := range steps {
		if dfs(s.Name) {
			return true
		}
	}
	return false
}

func renderTeamBundleYAML(spec teamBuilderSpec) string {
	var b strings.Builder

	fmt.Fprintf(&b, "apiVersion: agscale.io/v1alpha1\nkind: AgentWorkflow\nmetadata:\n  name: %s\n  namespace: %s\nspec:\n", spec.TeamName, spec.Namespace)
	fmt.Fprintf(&b, "  mode: RequestResponse\n")
	fmt.Fprintf(&b, "  entryAgentRef:\n    name: %s\n", spec.EntryAgent)
	if spec.ToolPolicy != "" || spec.BudgetPolicy != "" || spec.DataPolicy != "" {
		fmt.Fprintf(&b, "  policyRefs:\n")
		if spec.ToolPolicy != "" {
			fmt.Fprintf(&b, "    toolPolicyRef:\n      name: %s\n", spec.ToolPolicy)
		}
		if spec.BudgetPolicy != "" {
			fmt.Fprintf(&b, "    budgetPolicyRef:\n      name: %s\n", spec.BudgetPolicy)
		}
		if spec.DataPolicy != "" {
			fmt.Fprintf(&b, "    dataGovernancePolicyRef:\n      name: %s\n", spec.DataPolicy)
		}
	}
	fmt.Fprintf(&b, "  steps:\n")
	for _, s := range spec.Steps {
		fmt.Fprintf(&b, "    - name: %s\n", s.Name)
		fmt.Fprintf(&b, "      agentRef:\n        name: %s\n", s.Agent)
		if len(s.DependsOn) > 0 {
			fmt.Fprintf(&b, "      dependsOn:\n")
			for _, dep := range s.DependsOn {
				fmt.Fprintf(&b, "        - %s\n", dep)
			}
		}
		fmt.Fprintf(&b, "      type: %s\n", s.Type)
		fmt.Fprintf(&b, "      protocol: %s\n", s.Protocol)
		if s.EndpointPath != "" {
			fmt.Fprintf(&b, "      endpointPath: %s\n", s.EndpointPath)
		}
		fmt.Fprintf(&b, "      timeoutSeconds: %d\n", s.TimeoutSec)
	}
	fmt.Fprintf(&b, "  routing:\n    onSuccess: done\n    onFailure: failed\n")

	if spec.EnableEvent {
		fmt.Fprintf(&b, "---\napiVersion: agscale.io/v1alpha1\nkind: IntegrationEndpoint\nmetadata:\n  name: %s\n  namespace: %s\nspec:\n  type: webhook\n  connection:\n    url: %s\n    timeoutSeconds: 5\n  capabilities:\n    - healthcheck\n", spec.IntegrationName, spec.Namespace, spec.IntegrationURL)
		fmt.Fprintf(&b, "---\napiVersion: agscale.io/v1alpha1\nkind: EventSource\nmetadata:\n  name: %s-events\n  namespace: %s\n  annotations:\n    agscale.io/interval-seconds: \"%d\"\nspec:\n  integrationRef:\n    name: %s\n  topic: %s\n  delivery:\n    mode: Polling\n  targetWorkflowRef:\n    name: %s\n", spec.TeamName, spec.Namespace, spec.EventIntervalSec, spec.IntegrationName, spec.EventTopic, spec.TeamName)
	}
	return b.String()
}

func runKubectlApply(namespace, file string) error {
	cmd := exec.Command("kubectl", "apply", "-n", namespace, "-f", file)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createWorkflowRun(namespace, workflowName, runName string) error {
	client, _, err := kubeClients()
	if err != nil {
		return err
	}
	ctx := context.Background()
	run := &agscalev1alpha1.AgentWorkflowRun{
		ObjectMeta: metav1.ObjectMeta{Name: runName, Namespace: namespace},
		Spec: agscalev1alpha1.AgentWorkflowRunSpec{
			WorkflowRef:   agscalev1alpha1.LocalObjectReference{Name: workflowName},
			Trigger:       "manual",
			CorrelationID: fmt.Sprintf("%s-%d", workflowName, time.Now().Unix()),
		},
	}
	return client.Create(ctx, run)
}

func runTeamValidate(args []string) error {
	fs := flag.NewFlagSet("team validate", flag.ContinueOnError)
	file := fs.String("f", "", "workflow/team bundle YAML file")
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("usage: team validate -f <bundle.yaml> [-n namespace]")
	}

	clientCmd := exec.Command("kubectl", "apply", "--dry-run=client", "-n", *namespace, "-f", *file)
	clientOut, clientErr := clientCmd.CombinedOutput()
	if clientErr != nil {
		return fmt.Errorf("client dry-run failed:\n%s", strings.TrimSpace(string(clientOut)))
	}
	fmt.Println("[ok] kubectl client dry-run passed")

	serverCmd := exec.Command("kubectl", "apply", "--dry-run=server", "-n", *namespace, "-f", *file)
	serverOut, serverErr := serverCmd.CombinedOutput()
	if serverErr != nil {
		fmt.Printf("[warn] server dry-run failed:\n%s\n", strings.TrimSpace(string(serverOut)))
		fmt.Println("hint: ensure agscale CRDs/controller are installed and namespace exists")
		return nil
	}
	fmt.Println("[ok] kubectl server dry-run passed")
	return nil
}

func runTeamWatch(args []string) error {
	fs := flag.NewFlagSet("team watch", flag.ContinueOnError)
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	intervalSec := fs.Int("interval-seconds", 2, "poll interval seconds")
	timeoutSec := fs.Int("timeout-seconds", 0, "0 means no timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: team watch <run-name> [-n namespace] [--interval-seconds 2] [--timeout-seconds 0]")
	}
	client, _, err := kubeClients()
	if err != nil {
		return err
	}
	runName := rest[0]
	start := time.Now()

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		run := &agscalev1alpha1.AgentWorkflowRun{}
		err := client.Get(ctx, types.NamespacedName{Namespace: *namespace, Name: runName}, run)
		cancel()
		if err != nil {
			return err
		}
		renderTeamWatch(run)

		switch run.Status.Phase {
		case "Succeeded":
			fmt.Println("workflow run succeeded")
			return nil
		case "Failed":
			fmt.Println("workflow run failed")
			if len(run.Status.StepResults) > 0 {
				last := run.Status.StepResults[len(run.Status.StepResults)-1]
				fmt.Printf("last error: %s\n", last.Error)
				fmt.Printf("hint: %s\n", teamFailureHint(last.Error))
			}
			return nil
		}

		if *timeoutSec > 0 && time.Since(start) > time.Duration(*timeoutSec)*time.Second {
			return fmt.Errorf("watch timeout after %d seconds", *timeoutSec)
		}
		time.Sleep(time.Duration(*intervalSec) * time.Second)
	}
}

func renderTeamWatch(run *agscalev1alpha1.AgentWorkflowRun) {
	fmt.Print("\033[2J\033[H")
	fmt.Printf("AGScale Team Watch\nRun: %s/%s\nWorkflow: %s\nPhase: %s\n", run.Namespace, run.Name, run.Spec.WorkflowRef.Name, run.Status.Phase)
	if run.Status.StartedAt != nil {
		fmt.Printf("Started: %s\n", run.Status.StartedAt.Time.Format(time.RFC3339))
	}
	if run.Status.CompletedAt != nil {
		fmt.Printf("Completed: %s\n", run.Status.CompletedAt.Time.Format(time.RFC3339))
	}
	fmt.Println()
	if len(run.Status.PolicyDecisions) > 0 {
		fmt.Println("Policy Decisions:")
		for _, d := range run.Status.PolicyDecisions {
			fmt.Printf("- %s: %s (%s)\n", d.Policy, d.Result, d.Reason)
		}
		fmt.Println()
	}
	fmt.Println("Step Results:")
	fmt.Println("STEP\tSTATUS\tDURATION_MS\tERROR")
	for _, s := range run.Status.StepResults {
		fmt.Printf("%s\t%s\t%d\t%s\n", s.StepName, s.Status, s.DurationMs, s.Error)
	}
}

func teamFailureHint(errText string) string {
	text := strings.ToLower(errText)
	switch {
	case strings.Contains(text, "not found"):
		return "check referenced Agent/Policy resources and namespace"
	case strings.Contains(text, "timeout"):
		return "increase step timeoutSeconds or check target service health"
	case strings.Contains(text, "denied"):
		return "review tool/budget/data-governance policyRefs on AgentWorkflow"
	case strings.Contains(text, "connection"), strings.Contains(text, "dial tcp"):
		return "verify target agent service URL and networking"
	default:
		return "run './bin/agscale team status <run-name> -n <ns>' and inspect step error details"
	}
}

func promptString(reader *bufio.Reader, label, def string) (string, error) {
	if def == "" {
		fmt.Printf("%s: ", label)
	} else {
		fmt.Printf("%s [%s]: ", label, def)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(line)
	if v == "" {
		return def, nil
	}
	return v, nil
}

func promptInt(reader *bufio.Reader, label string, def, min, max int) (int, error) {
	for {
		s, err := promptString(reader, label, strconv.Itoa(def))
		if err != nil {
			return 0, err
		}
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			fmt.Println("please enter a valid integer")
			continue
		}
		if v < min || v > max {
			fmt.Printf("value must be between %d and %d\n", min, max)
			continue
		}
		return v, nil
	}
}

func promptBool(reader *bufio.Reader, label string, def bool) (bool, error) {
	defStr := "y/N"
	if def {
		defStr = "Y/n"
	}
	for {
		fmt.Printf("%s (%s): ", label, defStr)
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		v := strings.ToLower(strings.TrimSpace(line))
		if v == "" {
			return def, nil
		}
		if v == "y" || v == "yes" {
			return true, nil
		}
		if v == "n" || v == "no" {
			return false, nil
		}
		fmt.Println("please answer y or n")
	}
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
