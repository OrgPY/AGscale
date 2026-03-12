package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	agscalev1alpha1 "agscale/api/v1alpha1"
	"agscale/internal/fleet"
	"agscale/internal/fleet/inventory"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	tuiTargetSourceCluster = "cluster"
	tuiTargetSourceFleet   = "fleet"
)

var (
	errTUISelectionCancelled = errors.New("tui selection cancelled")

	loadCombinedTUITargetsFn = loadCombinedTUITargets
	selectTUIAgentTargetFn   = selectTUIAgentTarget
	openClusterTUITargetFn   = openClusterTUITarget
	openFleetTUITargetFn     = runInventoryAgentTUIChatFirst
)

type tuiAgentTarget struct {
	ID            string
	Name          string
	Namespace     string
	Host          string
	DisplayName   string
	Source        string
	Protocol      string
	Endpoint      string
	Health        string
	BudgetPhase   string
	ReadyReplicas int32
	ChatPath      string
	Model         string
}

func runTUI(args []string) error {
	namespace, inventoryOverride, rest, err := parseTUIArgs(args)
	if err != nil {
		return err
	}
	if len(rest) > 0 && rest[0] == "agent" {
		if len(rest) != 2 {
			return errors.New("usage: tui agent <name> [-n namespace]")
		}
		warnDeprecated("agscale tui agent <name>", "agscale cluster tui agent <name>")
		return runClusterTUIFn([]string{"agent", rest[1], "-n", namespace})
	}
	if len(rest) > 1 {
		return errors.New("usage: tui [<agent-id-or-name>] [--namespace default] [--inventory path]")
	}

	targets, warnings, inventoryPath, err := loadCombinedTUITargetsFn(namespace, inventoryOverride)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		msg := "no agents available"
		if len(warnings) > 0 {
			msg += " (" + strings.Join(warnings, "; ") + ")"
		}
		msg += "; try 'agscale cluster doctor -n " + namespace + "' or 'agscale fleet scan --local --write'"
		return errors.New(msg)
	}

	selected := tuiAgentTarget{}
	switch len(rest) {
	case 0:
		selected, err = selectTUIAgentTargetFn(targets, warnings, "")
	case 1:
		matches := resolveDirectTUITargets(targets, rest[0])
		switch len(matches) {
		case 1:
			selected = matches[0]
		case 0:
			partial := filterTUITargets(targets, rest[0])
			if len(partial) == 0 {
				return fmt.Errorf("agent %q not found; try 'agscale tui' to browse available agents", rest[0])
			}
			selected, err = selectTUIAgentTargetFn(partial, warnings, rest[0])
		default:
			selected, err = selectTUIAgentTargetFn(matches, warnings, rest[0])
		}
	}
	if err != nil {
		if errors.Is(err, errTUISelectionCancelled) {
			return nil
		}
		return err
	}

	switch selected.Source {
	case tuiTargetSourceCluster:
		return openClusterTUITargetFn(selected)
	case tuiTargetSourceFleet:
		return openFleetTUITargetFn(selected, inventoryPath)
	default:
		return fmt.Errorf("unsupported target source %q", selected.Source)
	}
}

func parseTUIArgs(args []string) (namespace, inventoryOverride string, rest []string, err error) {
	namespace = defaultNamespace
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-n" || args[i] == "--namespace":
			if i+1 >= len(args) {
				return "", "", nil, errors.New("usage: tui [<agent-id-or-name>] [--namespace default] [--inventory path]")
			}
			namespace = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-n="):
			namespace = strings.TrimPrefix(args[i], "-n=")
		case strings.HasPrefix(args[i], "--namespace="):
			namespace = strings.TrimPrefix(args[i], "--namespace=")
		case args[i] == "--inventory":
			if i+1 >= len(args) {
				return "", "", nil, errors.New("usage: tui [<agent-id-or-name>] [--namespace default] [--inventory path]")
			}
			inventoryOverride = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--inventory="):
			inventoryOverride = strings.TrimPrefix(args[i], "--inventory=")
		default:
			rest = append(rest, args[i])
		}
	}
	return namespace, inventoryOverride, rest, nil
}

func loadCombinedTUITargets(namespace, inventoryOverride string) ([]tuiAgentTarget, []string, string, error) {
	inventoryPath, err := resolveInventoryPath(inventoryOverride)
	if err != nil {
		return nil, nil, "", err
	}

	targets := []tuiAgentTarget{}
	warnings := []string{}

	clusterTargets, clusterWarnings := loadClusterTUITargets(namespace)
	targets = append(targets, clusterTargets...)
	warnings = append(warnings, clusterWarnings...)

	fleetTargets, err := loadInventoryTUITargets(inventoryPath)
	if err != nil {
		return nil, warnings, inventoryPath, err
	}
	targets = append(targets, fleetTargets...)

	sortTUITargets(targets)
	return targets, warnings, inventoryPath, nil
}

func loadClusterTUITargets(namespace string) ([]tuiAgentTarget, []string) {
	client, _, err := kubeClients()
	if err != nil {
		return nil, []string{fmt.Sprintf("cluster agents unavailable: %v", err)}
	}

	ctx := context.Background()
	var list agscalev1alpha1.AgentList
	if err := client.List(ctx, &list, ctrlclient.InNamespace(namespace)); err != nil {
		return nil, []string{fmt.Sprintf("cluster agents unavailable: %v", err)}
	}

	targets := make([]tuiAgentTarget, 0, len(list.Items))
	for _, agent := range list.Items {
		targets = append(targets, newClusterTUITarget(&agent))
	}
	return targets, nil
}

func loadInventoryTUITargets(inventoryPath string) ([]tuiAgentTarget, error) {
	inv, err := inventory.Load(inventoryPath)
	if err != nil {
		return nil, err
	}
	targets := make([]tuiAgentTarget, 0, len(inv.Agents))
	for _, agent := range inv.Agents {
		targets = append(targets, newFleetTUITarget(agent))
	}
	return targets, nil
}

func newClusterTUITarget(agent *agscalev1alpha1.Agent) tuiAgentTarget {
	target := tuiAgentTarget{
		ID:            fmt.Sprintf("cluster:%s/%s", agent.Namespace, agent.Name),
		Name:          agent.Name,
		Namespace:     agent.Namespace,
		DisplayName:   fmt.Sprintf("%s [%s %s]", agent.Name, tuiTargetSourceCluster, agent.Namespace),
		Source:        tuiTargetSourceCluster,
		Protocol:      "http",
		Endpoint:      agent.Status.URL,
		BudgetPhase:   agent.Status.BudgetPhase,
		ReadyReplicas: agent.Status.ReadyReplicas,
		Health:        clusterTargetHealth(agent.Status.ReadyReplicas),
		ChatPath:      agent.Annotations["agscale.io/chat-path"],
	}
	if target.ChatPath == "" {
		target.ChatPath = "/chat"
	}
	if strings.TrimSpace(target.Endpoint) == "" {
		target.Protocol = ""
	}
	return target
}

func clusterTargetHealth(ready int32) string {
	if ready > 0 {
		return fleet.HealthHealthy
	}
	return fleet.HealthUnknown
}

func newFleetTUITarget(agent fleet.AgentRecord) tuiAgentTarget {
	chatPath := "/chat"
	model := ""
	if agent.Metadata != nil {
		if v := strings.TrimSpace(agent.Metadata["chatPath"]); v != "" {
			chatPath = v
		}
		if v := strings.TrimSpace(agent.Metadata["model"]); v != "" {
			model = v
		} else if v := strings.TrimSpace(agent.Metadata["defaultModel"]); v != "" {
			model = v
		}
	}
	display := agent.Name
	if display == "" {
		display = agent.ID
	}
	scope := strings.TrimSpace(agent.Host)
	if scope == "" {
		scope = "inventory"
	}
	return tuiAgentTarget{
		ID:          agent.ID,
		Name:        display,
		Host:        agent.Host,
		DisplayName: fmt.Sprintf("%s [%s %s]", display, tuiTargetSourceFleet, scope),
		Source:      tuiTargetSourceFleet,
		Protocol:    agent.Protocol,
		Endpoint:    agent.Endpoint,
		Health:      agent.Health,
		ChatPath:    chatPath,
		Model:       model,
	}
}

func sortTUITargets(targets []tuiAgentTarget) {
	sort.Slice(targets, func(i, j int) bool {
		left := targets[i]
		right := targets[j]
		if left.Source != right.Source {
			return targetSourceRank(left.Source) < targetSourceRank(right.Source)
		}
		if leftHealth, rightHealth := targetHealthRank(left.Health), targetHealthRank(right.Health); leftHealth != rightHealth {
			return leftHealth < rightHealth
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		return left.DisplayName < right.DisplayName
	})
}

func targetSourceRank(source string) int {
	if source == tuiTargetSourceCluster {
		return 0
	}
	return 1
}

func targetHealthRank(health string) int {
	switch strings.ToLower(strings.TrimSpace(health)) {
	case fleet.HealthHealthy:
		return 0
	case fleet.HealthDegraded:
		return 1
	default:
		return 2
	}
}

func resolveDirectTUITargets(targets []tuiAgentTarget, query string) []tuiAgentTarget {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}

	clusterMatches := []tuiAgentTarget{}
	for _, target := range targets {
		if target.Source != tuiTargetSourceCluster {
			continue
		}
		if strings.EqualFold(target.ID, q) || strings.EqualFold(target.qualifiedName(), q) || strings.EqualFold(target.Name, q) {
			clusterMatches = append(clusterMatches, target)
		}
	}
	if len(clusterMatches) > 0 {
		return clusterMatches
	}

	fleetMatches := []tuiAgentTarget{}
	for _, target := range targets {
		if target.Source != tuiTargetSourceFleet {
			continue
		}
		if strings.EqualFold(target.ID, q) || strings.EqualFold(target.Name, q) {
			fleetMatches = append(fleetMatches, target)
		}
	}
	return fleetMatches
}

func filterTUITargets(targets []tuiAgentTarget, filter string) []tuiAgentTarget {
	q := strings.ToLower(strings.TrimSpace(filter))
	if q == "" {
		out := make([]tuiAgentTarget, len(targets))
		copy(out, targets)
		return out
	}

	out := make([]tuiAgentTarget, 0, len(targets))
	for _, target := range targets {
		values := []string{
			target.ID,
			target.Name,
			target.Namespace,
			target.Host,
			target.DisplayName,
			target.Endpoint,
			target.Source,
		}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), q) {
				out = append(out, target)
				break
			}
		}
	}
	return out
}

func selectTUIAgentTarget(targets []tuiAgentTarget, warnings []string, initialFilter string) (tuiAgentTarget, error) {
	reader := bufio.NewReader(os.Stdin)
	filter := strings.TrimSpace(initialFilter)
	info := ""

	for {
		filtered := filterTUITargets(targets, filter)
		renderTUISelector(filtered, warnings, filter, info)

		fmt.Print("select> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return tuiAgentTarget{}, err
		}
		input := strings.TrimSpace(line)
		switch {
		case input == "":
			if len(filtered) == 1 {
				return filtered[0], nil
			}
			info = "enter a number, /filter, or q"
		case input == "q" || input == "/q" || input == "/quit" || input == "/exit":
			return tuiAgentTarget{}, errTUISelectionCancelled
		case strings.HasPrefix(input, "/"):
			filter = strings.TrimSpace(strings.TrimPrefix(input, "/"))
			if len(filterTUITargets(targets, filter)) == 0 {
				info = "no agents match that filter"
			} else if filter == "" {
				info = "filter cleared"
			} else {
				info = "filter updated"
			}
		default:
			idx, err := strconv.Atoi(input)
			if err != nil {
				info = "enter a number, /filter, or q"
				continue
			}
			if idx < 1 || idx > len(filtered) {
				info = fmt.Sprintf("choose a number between 1 and %d", len(filtered))
				continue
			}
			return filtered[idx-1], nil
		}
	}
}

func renderTUISelector(targets []tuiAgentTarget, warnings []string, filter, info string) {
	fmt.Print("\033[2J\033[H")
	fmt.Println("AGScale TUI")
	fmt.Println("-----------")
	for _, warning := range warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	if filter != "" {
		fmt.Printf("filter: %q\n", filter)
	}
	if info != "" {
		fmt.Printf("info: %s\n", info)
	}
	fmt.Println()

	if len(targets) == 0 {
		fmt.Println("No agents match the current filter.")
		fmt.Println()
		fmt.Println("Type / to clear the filter or q to quit.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tSOURCE\tTARGET\tSTATUS\tENDPOINT")
	for i, target := range targets {
		fmt.Fprintf(
			tw,
			"%d\t%s\t%s\t%s\t%s\n",
			i+1,
			target.Source,
			target.selectorTarget(),
			target.selectorStatus(),
			truncate(target.Endpoint, 42),
		)
	}
	_ = tw.Flush()
	fmt.Println()
	fmt.Println("Enter a number to open, /text to filter, or q to quit.")
}

func (t tuiAgentTarget) qualifiedName() string {
	if t.Namespace == "" {
		return t.Name
	}
	return t.Namespace + "/" + t.Name
}

func (t tuiAgentTarget) selectorTarget() string {
	if t.Source == tuiTargetSourceCluster {
		return t.qualifiedName()
	}
	if t.Host != "" {
		return fmt.Sprintf("%s@%s", t.Name, t.Host)
	}
	return t.Name
}

func (t tuiAgentTarget) selectorStatus() string {
	parts := []string{}
	if t.Source == tuiTargetSourceCluster {
		parts = append(parts, fmt.Sprintf("ready=%d", t.ReadyReplicas))
		if t.BudgetPhase != "" {
			parts = append(parts, "budget="+t.BudgetPhase)
		}
	} else if t.Health != "" {
		parts = append(parts, "health="+t.Health)
	}
	if t.Protocol != "" {
		parts = append(parts, "proto="+t.Protocol)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func (t tuiAgentTarget) statusSummary() string {
	parts := []string{"source=" + t.Source}
	if t.Namespace != "" {
		parts = append(parts, "namespace="+t.Namespace)
	}
	if t.Source == tuiTargetSourceCluster {
		parts = append(parts, fmt.Sprintf("ready=%d", t.ReadyReplicas))
		if t.BudgetPhase != "" {
			parts = append(parts, "budget="+t.BudgetPhase)
		}
	} else if t.Health != "" {
		parts = append(parts, "health="+t.Health)
	}
	if t.Protocol != "" {
		parts = append(parts, "proto="+t.Protocol)
	}
	return strings.Join(parts, "  ")
}

func openClusterTUITarget(target tuiAgentTarget) error {
	client, kc, err := kubeClients()
	if err != nil {
		return err
	}
	return runAgentTUIChatFirst(client, kc, target.Namespace, target.Name)
}

func runInventoryAgentTUIChatFirst(target tuiAgentTarget, inventoryPath string) error {
	reader := bufio.NewReader(os.Stdin)
	pf := &tuiPortForwardSession{}
	defer pf.close()

	transcript := []tuiChatLine{{
		role: "system",
		text: "Chat-first mode active. Type directly to chat, or use /help for actions.",
	}}
	showRaw := false
	info := ""
	current := target

	for {
		renderChatFirstScreen(current, transcript, info, showRaw, []string{"/refresh", "/raw", "/quit"})
		fmt.Print("chat> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			cmd, _ := splitCommand(input)
			switch cmd {
			case "/q", "/quit", "/exit":
				return nil
			case "/help":
				info = "Commands: /refresh /raw /quit"
			case "/raw":
				showRaw = !showRaw
				info = fmt.Sprintf("raw-json view: %t", showRaw)
			case "/refresh":
				refreshed, err := refreshInventoryTUITarget(inventoryPath, current)
				if err != nil {
					info = "refresh failed: " + err.Error()
				} else {
					current = refreshed
					info = "refreshed"
				}
			default:
				info = "unknown command, try /help"
			}
			continue
		}

		transcript = append(transcript, tuiChatLine{role: "user", text: input})
		resp, err := sendTUIChatMessage(current, input, pf)
		if err != nil {
			transcript = append(transcript, tuiChatLine{
				role: "system",
				text: "chat error: " + err.Error(),
			})
			info = "chat failed"
			continue
		}
		transcript = append(transcript, tuiChatLine{
			role: "assistant",
			text: resp.text,
			raw:  resp.raw,
			meta: fmt.Sprintf("status=%d latency=%s", resp.statusCode, resp.latency.Round(10*time.Millisecond)),
		})
		info = "chat ok"
	}
}

func refreshInventoryTUITarget(inventoryPath string, target tuiAgentTarget) (tuiAgentTarget, error) {
	targets, err := loadInventoryTUITargets(inventoryPath)
	if err != nil {
		return tuiAgentTarget{}, err
	}
	for _, candidate := range targets {
		if candidate.ID == target.ID {
			return candidate, nil
		}
	}
	return tuiAgentTarget{}, fmt.Errorf("agent %q not found in inventory %s", target.ID, inventoryPath)
}
