package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	agscalev1alpha1 "agscale/api/v1alpha1"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type commandCenterCandidate struct {
	Agent     agscalev1alpha1.Agent
	SkillHits int
}

func runTeamCommandCenter(args []string) error {
	fs := flag.NewFlagSet("team command-center", flag.ContinueOnError)
	namespace := fs.String("n", defaultNamespace, "namespace")
	fs.StringVar(namespace, "namespace", defaultNamespace, "namespace")
	out := fs.String("out", "", "output workflow bundle file")
	maxAgents := fs.Int("max-agents", 3, "maximum number of agents to schedule (subset only)")
	topology := fs.String("topology", "dag", "team topology: dag|sequential")
	explain := fs.Bool("explain-selection", false, "print detailed selection decisions for each candidate agent")
	selectorRaw := fs.String("selector", "", "label selector k=v,k2=v2 applied to Agent metadata labels")
	includeAgentsRaw := fs.String("agents", "", "explicit include list: agent-a,agent-b")
	excludeAgentsRaw := fs.String("exclude-agents", "", "exclude list: agent-x,agent-y")
	requiredSkillsRaw := fs.String("require-skills", "", "required skill refs: web-search,db-query")
	entryAgent := fs.String("entry-agent", "", "entry agent name (must be in selected set; default first selected)")
	protocol := fs.String("protocol", "http", "step protocol: http|mcp|a2a")
	applyNow := fs.Bool("apply", false, "apply generated bundle with kubectl")
	createRun := fs.Bool("run", false, "create first AgentWorkflowRun after apply")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: team command-center <task-name> [-n namespace] [--selector k=v] [--agents a,b] [--exclude-agents x,y] [--require-skills s1,s2] [--max-agents 3] [--topology dag|sequential] [--entry-agent name] [--protocol http|mcp|a2a] [--out file] [--apply] [--run]")
	}
	if *maxAgents <= 0 {
		return errors.New("--max-agents must be > 0")
	}
	topo := strings.ToLower(strings.TrimSpace(*topology))
	if topo != "dag" && topo != "sequential" {
		return fmt.Errorf("unsupported topology %q (use dag|sequential)", *topology)
	}
	p := strings.ToLower(strings.TrimSpace(*protocol))
	if p != "http" && p != "mcp" && p != "a2a" {
		return fmt.Errorf("unsupported protocol %q (use http|mcp|a2a)", *protocol)
	}
	if *createRun && !*applyNow {
		return errors.New("--run requires --apply so generated workflow exists before creating AgentWorkflowRun")
	}

	taskName := sanitizeName(rest[0])
	workflowName := fmt.Sprintf("%s-command-center", taskName)
	if *out == "" {
		*out = fmt.Sprintf("%s-team.yaml", workflowName)
	}

	labelSelector, err := parseSimpleLabelSelector(*selectorRaw)
	if err != nil {
		return err
	}
	includeSet := csvToSet(*includeAgentsRaw)
	excludeSet := csvToSet(*excludeAgentsRaw)
	requiredSkills := csvToList(*requiredSkillsRaw)

	client, _, err := kubeClients()
	if err != nil {
		return err
	}
	ctx := context.Background()
	var list agscalev1alpha1.AgentList
	if err := client.List(ctx, &list, ctrlclient.InNamespace(*namespace)); err != nil {
		return err
	}

	candidates := make([]commandCenterCandidate, 0, len(list.Items))
	decisionLog := make([]string, 0, len(list.Items))
	for _, a := range list.Items {
		droppedReasons := []string{}
		if len(includeSet) > 0 && !includeSet[a.Name] {
			droppedReasons = append(droppedReasons, "not in --agents allowlist")
		}
		if excludeSet[a.Name] {
			droppedReasons = append(droppedReasons, "matched --exclude-agents")
		}
		if len(droppedReasons) == 0 && !matchLabels(a.Labels, labelSelector) {
			droppedReasons = append(droppedReasons, "label selector mismatch")
		}
		if len(droppedReasons) == 0 && a.Status.ReadyReplicas <= 0 {
			droppedReasons = append(droppedReasons, "not ready (readyReplicas=0)")
		}
		if len(droppedReasons) == 0 && strings.TrimSpace(a.Status.URL) == "" {
			droppedReasons = append(droppedReasons, "status.url empty")
		}
		hits, ok := agentMatchesSkills(a, requiredSkills)
		if len(droppedReasons) == 0 && !ok {
			droppedReasons = append(droppedReasons, "missing required skills")
		}
		if len(droppedReasons) > 0 {
			if *explain {
				decisionLog = append(decisionLog, fmt.Sprintf("- %s: dropped (%s)", a.Name, strings.Join(droppedReasons, "; ")))
			}
			continue
		}
		if *explain {
			decisionLog = append(decisionLog, fmt.Sprintf("- %s: eligible (readyReplicas=%d, skillHits=%d)", a.Name, a.Status.ReadyReplicas, hits))
		}
		candidates = append(candidates, commandCenterCandidate{Agent: a, SkillHits: hits})
	}
	if len(candidates) == 0 {
		if *explain && len(decisionLog) > 0 {
			sort.Strings(decisionLog)
			fmt.Println("selection decisions:")
			for _, line := range decisionLog {
				fmt.Println(line)
			}
		}
		return errors.New("no eligible agents found after filters (check readiness, selector, skills, and include/exclude lists)")
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].SkillHits != candidates[j].SkillHits {
			return candidates[i].SkillHits > candidates[j].SkillHits
		}
		if candidates[i].Agent.Status.ReadyReplicas != candidates[j].Agent.Status.ReadyReplicas {
			return candidates[i].Agent.Status.ReadyReplicas > candidates[j].Agent.Status.ReadyReplicas
		}
		return candidates[i].Agent.Name < candidates[j].Agent.Name
	})

	chosen := candidates
	if len(chosen) > *maxAgents {
		chosen = chosen[:*maxAgents]
	}
	if len(chosen) == 0 {
		return errors.New("no agents selected for team")
	}

	selectedNames := make([]string, 0, len(chosen))
	for _, c := range chosen {
		selectedNames = append(selectedNames, c.Agent.Name)
	}

	entry := strings.TrimSpace(*entryAgent)
	if entry == "" {
		entry = selectedNames[0]
	} else if !containsStringValue(selectedNames, entry) {
		return fmt.Errorf("--entry-agent %q is not in selected team: %s", entry, strings.Join(selectedNames, ","))
	}

	spec := teamBuilderSpec{
		TeamName:   workflowName,
		Namespace:  *namespace,
		Profile:    "openclaw",
		EntryAgent: entry,
		OutFile:    *out,
		Apply:      *applyNow,
		CreateRun:  *createRun,
	}
	spec.Steps, err = buildCommandCenterSteps(selectedNames, p, topo)
	if err != nil {
		return err
	}

	if err := validateTeamBuilderSpec(spec); err != nil {
		return err
	}

	bundle := renderTeamBundleYAML(spec)
	if spec.CreateRun {
		runName := fmt.Sprintf("%s-run-%d", spec.TeamName, time.Now().Unix())
		bundle = fmt.Sprintf("%s\n---\napiVersion: agscale.io/v1alpha1\nkind: AgentWorkflowRun\nmetadata:\n  name: %s\n  namespace: %s\nspec:\n  workflowRef:\n    name: %s\n  trigger: command-center\n  correlationID: %s\n", strings.TrimSpace(bundle), runName, spec.Namespace, spec.TeamName, taskName)
	}
	if err := os.WriteFile(spec.OutFile, []byte(bundle+"\n"), 0644); err != nil {
		return err
	}

	fmt.Printf("command-center selected %d/%d candidate agents for task %q (topology=%s)\n", len(selectedNames), len(candidates), rest[0], topo)
	for idx, c := range chosen {
		fmt.Printf("  %d. %s (readyReplicas=%d, skillHits=%d)\n", idx+1, c.Agent.Name, c.Agent.Status.ReadyReplicas, c.SkillHits)
	}
	if *explain && len(decisionLog) > 0 {
		sort.Strings(decisionLog)
		fmt.Println("selection decisions:")
		for _, line := range decisionLog {
			fmt.Println(line)
		}
	}
	fmt.Printf("generated workflow bundle: %s\n", spec.OutFile)

	if spec.Apply {
		if err := runKubectlApply(spec.Namespace, spec.OutFile); err != nil {
			return err
		}
		fmt.Printf("applied bundle to namespace %q\n", spec.Namespace)
	}
	return nil
}

func buildCommandCenterSteps(selected []string, protocol string, topology string) ([]teamBuilderStep, error) {
	if len(selected) == 0 {
		return nil, errors.New("no selected agents to build workflow")
	}
	if topology == "sequential" {
		steps := make([]teamBuilderStep, 0, len(selected))
		for i, name := range selected {
			step := teamBuilderStep{
				Name:       fmt.Sprintf("step-%02d-%s", i+1, sanitizeName(name)),
				Agent:      name,
				Type:       normalizeStepType(defaultStepType(i)),
				Protocol:   protocol,
				TimeoutSec: 20,
			}
			if i > 0 {
				step.DependsOn = []string{steps[i-1].Name}
			}
			steps = append(steps, step)
		}
		return steps, nil
	}

	// DAG topology: triage -> retrieve/solve/specialists -> response.
	steps := make([]teamBuilderStep, 0, len(selected)+1)
	triageName := "triage"
	steps = append(steps, teamBuilderStep{
		Name:       triageName,
		Agent:      selected[0],
		Type:       "Prompt",
		Protocol:   protocol,
		TimeoutSec: 20,
	})
	if len(selected) == 1 {
		return steps, nil
	}

	retrieveName := ""
	if len(selected) >= 2 {
		retrieveName = "retrieve"
		steps = append(steps, teamBuilderStep{
			Name:       retrieveName,
			Agent:      selected[1],
			Type:       "Retrieve",
			Protocol:   protocol,
			TimeoutSec: 20,
			DependsOn:  []string{triageName},
		})
	}

	solveName := ""
	if len(selected) >= 4 {
		solveName = "solve"
		deps := []string{triageName}
		if retrieveName != "" {
			deps = append(deps, retrieveName)
		}
		steps = append(steps, teamBuilderStep{
			Name:       solveName,
			Agent:      selected[2],
			Type:       "Transform",
			Protocol:   protocol,
			TimeoutSec: 20,
			DependsOn:  deps,
		})
	}

	specialistDeps := []string{}
	if len(selected) > 4 {
		for i := 3; i < len(selected)-1; i++ {
			name := fmt.Sprintf("specialist-%02d", i-2)
			deps := []string{triageName}
			if retrieveName != "" {
				deps = append(deps, retrieveName)
			}
			if solveName != "" {
				deps = append(deps, solveName)
			}
			steps = append(steps, teamBuilderStep{
				Name:       name,
				Agent:      selected[i],
				Type:       "ToolCall",
				Protocol:   protocol,
				TimeoutSec: 20,
				DependsOn:  deps,
			})
			specialistDeps = append(specialistDeps, name)
		}
	}

	finalDeps := []string{}
	switch {
	case len(specialistDeps) > 0:
		finalDeps = append(finalDeps, specialistDeps...)
	case solveName != "":
		finalDeps = []string{solveName}
	case retrieveName != "":
		finalDeps = []string{retrieveName}
	default:
		finalDeps = []string{triageName}
	}
	steps = append(steps, teamBuilderStep{
		Name:       "response",
		Agent:      selected[len(selected)-1],
		Type:       "Transform",
		Protocol:   protocol,
		TimeoutSec: 20,
		DependsOn:  finalDeps,
	})
	return steps, nil
}

func parseSimpleLabelSelector(v string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range strings.Split(strings.TrimSpace(v), ",") {
		part := strings.TrimSpace(p)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" || strings.TrimSpace(kv[1]) == "" {
			return nil, fmt.Errorf("invalid selector %q; expected k=v,k2=v2", v)
		}
		out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return out, nil
}

func matchLabels(labels map[string]string, expected map[string]string) bool {
	if len(expected) == 0 {
		return true
	}
	for k, v := range expected {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func agentMatchesSkills(agent agscalev1alpha1.Agent, required []string) (int, bool) {
	if len(required) == 0 {
		return len(agent.Spec.SkillRefs), true
	}
	have := map[string]bool{}
	for _, s := range agent.Spec.SkillRefs {
		if s.Name != "" {
			have[s.Name] = true
		}
	}
	hits := 0
	for _, r := range required {
		if !have[r] {
			return 0, false
		}
		hits++
	}
	return hits, true
}

func csvToSet(v string) map[string]bool {
	out := map[string]bool{}
	for _, item := range csvToList(v) {
		out[item] = true
	}
	return out
}

func csvToList(v string) []string {
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

func containsStringValue(items []string, target string) bool {
	for _, i := range items {
		if i == target {
			return true
		}
	}
	return false
}

func sanitizeName(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	replacer := strings.NewReplacer(" ", "-", "_", "-", "/", "-", ".", "-", ":", "-", "@", "-")
	s = replacer.Replace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "task-" + strconv.FormatInt(time.Now().Unix(), 10)
	}
	return out
}
