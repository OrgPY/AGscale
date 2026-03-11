package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"agscale/internal/fleet"
	"agscale/internal/fleet/discovery"
	"agscale/internal/fleet/inventory"
	"agscale/internal/fleet/scheduler"
)

func runFleet(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fleet <scan|list|run|inspect>")
	}
	switch args[0] {
	case "scan":
		return runFleetScan(args[1:])
	case "list":
		return runFleetList(args[1:])
	case "run":
		return runFleetRun(args[1:])
	case "inspect":
		return runFleetInspect(args[1:])
	default:
		return fmt.Errorf("unknown fleet subcommand %q", args[0])
	}
}

func runFleetScan(args []string) error {
	fs := flag.NewFlagSet("fleet scan", flag.ContinueOnError)
	local := fs.Bool("local", true, "scan localhost")
	hostsRaw := fs.String("hosts", "", "ssh hosts, comma-separated")
	sshCfg := fs.String("ssh-config", "~/.ssh/config", "ssh config path")
	portsRaw := fs.String("ports", "18789,8080,8000,3000,5000,11434", "port candidates")
	timeout := fs.Int("timeout-seconds", 3, "probe timeout seconds")
	output := fs.String("output", "table", "output format: table|json")
	write := fs.Bool("write", false, "persist scan result to inventory")
	path := fs.String("inventory", "", "inventory path (default ~/.agscale/inventory/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	invPath, err := resolveInventoryPath(*path)
	if err != nil {
		return err
	}
	ports, err := parsePorts(*portsRaw)
	if err != nil {
		return err
	}
	opts := fleet.ProbeOptions{TimeoutSeconds: *timeout}
	found := []fleet.AgentRecord{}

	if *local {
		endpoints := discovery.LocalDefaultEndpoints()
		if env := strings.TrimSpace(os.Getenv("AGSCALE_DISCOVERY_ENDPOINTS")); env != "" {
			endpoints = append(endpoints, csvToList(env)...)
		}
		found = append(found, discovery.ScanLocal(endpoints, opts)...)
	}

	hosts := csvToList(*hostsRaw)
	if len(hosts) > 0 {
		found = append(found, discovery.ScanSSHHosts(hosts, expandHome(*sshCfg), ports, opts)...)
	}

	// de-duplicate by id
	uniq := map[string]fleet.AgentRecord{}
	for _, a := range found {
		uniq[a.ID] = a
	}
	found = found[:0]
	for _, a := range uniq {
		found = append(found, a)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].ID < found[j].ID })

	if *write {
		existing, err := inventory.Load(invPath)
		if err != nil {
			return err
		}
		merged := inventory.Merge(existing, found)
		if err := inventory.Save(invPath, merged); err != nil {
			return err
		}
		fmt.Printf("inventory saved: %s (agents=%d)\n", invPath, len(merged.Agents))
	}

	return printFleetAgents(found, *output)
}

func runFleetList(args []string) error {
	fs := flag.NewFlagSet("fleet list", flag.ContinueOnError)
	status := fs.String("status", "all", "healthy|degraded|unknown|all")
	protocol := fs.String("protocol", "all", "http|mcp|all")
	host := fs.String("host", "", "host filter")
	skillsRaw := fs.String("skills", "", "required skills s1,s2")
	output := fs.String("output", "table", "output format: table|json")
	path := fs.String("inventory", "", "inventory path (default ~/.agscale/inventory/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	invPath, err := resolveInventoryPath(*path)
	if err != nil {
		return err
	}
	inv, err := inventory.Load(invPath)
	if err != nil {
		return err
	}
	requiredSkills := csvToList(*skillsRaw)
	filtered := filterFleetAgents(inv.Agents, *status, *protocol, *host, requiredSkills)
	return printFleetAgents(filtered, *output)
}

func runFleetInspect(args []string) error {
	fs := flag.NewFlagSet("fleet inspect", flag.ContinueOnError)
	path := fs.String("inventory", "", "inventory path (default ~/.agscale/inventory/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: fleet inspect <agent-id-or-name>")
	}
	invPath, err := resolveInventoryPath(*path)
	if err != nil {
		return err
	}
	inv, err := inventory.Load(invPath)
	if err != nil {
		return err
	}
	target := strings.TrimSpace(rest[0])
	for _, a := range inv.Agents {
		if a.ID == target || a.Name == target {
			b, _ := json.MarshalIndent(a, "", "  ")
			fmt.Println(string(b))
			return nil
		}
	}
	return fmt.Errorf("agent %q not found in inventory %s", target, invPath)
}

func runFleetRun(args []string) error {
	fs := flag.NewFlagSet("fleet run", flag.ContinueOnError)
	agentsRaw := fs.String("agents", "", "explicit include list by id or name")
	excludeRaw := fs.String("exclude-agents", "", "exclude list by id or name")
	selectorRaw := fs.String("selector", "", "label selector k=v,k2=v2")
	requiredSkillsRaw := fs.String("require-skills", "", "required skills s1,s2")
	maxAgents := fs.Int("max-agents", 3, "max selected agents")
	topology := fs.String("topology", "dag", "dag|sequential")
	explain := fs.Bool("explain-selection", false, "print why each agent selected/skipped")
	dryRun := fs.Bool("dry-run", false, "print plan only, do not invoke endpoints")
	timeout := fs.Int("timeout-seconds", 20, "step invoke timeout")
	output := fs.String("output", "table", "output format: table|json")
	path := fs.String("inventory", "", "inventory path (default ~/.agscale/inventory/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: fleet run <task-name> [--agents a,b] [--exclude-agents x,y] [--selector k=v] [--require-skills s1,s2] [--max-agents 3] [--topology dag|sequential] [--explain-selection] [--dry-run]")
	}
	if *maxAgents <= 0 {
		return errors.New("--max-agents must be > 0")
	}
	task := rest[0]
	invPath, err := resolveInventoryPath(*path)
	if err != nil {
		return err
	}
	inv, err := inventory.Load(invPath)
	if err != nil {
		return err
	}
	if len(inv.Agents) == 0 {
		return fmt.Errorf("inventory empty: %s (run 'agscale fleet scan --local --write' first)", invPath)
	}

	includeSet := csvToSet(*agentsRaw)
	excludeSet := csvToSet(*excludeRaw)
	requiredSkills := csvToList(*requiredSkillsRaw)
	labelSelector, err := parseSimpleLabelSelector(*selectorRaw)
	if err != nil {
		return err
	}

	candidates := []fleet.AgentRecord{}
	decisionLog := []string{}
	for _, a := range inv.Agents {
		reasons := []string{}
		if len(includeSet) > 0 && !matchIDOrName(includeSet, a) {
			reasons = append(reasons, "not in --agents allowlist")
		}
		if matchIDOrName(excludeSet, a) {
			reasons = append(reasons, "matched --exclude-agents")
		}
		if len(reasons) == 0 && a.Health != fleet.HealthHealthy {
			reasons = append(reasons, "agent not healthy")
		}
		if len(reasons) == 0 && !matchLabels(a.Labels, labelSelector) {
			reasons = append(reasons, "label selector mismatch")
		}
		if len(reasons) == 0 {
			if _, ok := fleetSkillMatch(a, requiredSkills); !ok {
				reasons = append(reasons, "missing required skills")
			}
		}
		if len(reasons) > 0 {
			if *explain {
				decisionLog = append(decisionLog, fmt.Sprintf("- %s: dropped (%s)", a.Name, strings.Join(reasons, "; ")))
			}
			continue
		}
		if *explain {
			decisionLog = append(decisionLog, fmt.Sprintf("- %s: eligible (%s %s)", a.Name, a.Protocol, a.Endpoint))
		}
		candidates = append(candidates, a)
	}
	if len(candidates) == 0 {
		if *explain {
			sort.Strings(decisionLog)
			for _, line := range decisionLog {
				fmt.Println(line)
			}
		}
		return errors.New("no eligible agents after filtering")
	}
	selected := scheduler.RankAgents(candidates, requiredSkills, *maxAgents)
	if len(selected) == 0 {
		return errors.New("no selected agents")
	}
	plan := scheduler.BuildPlan(selected, strings.ToLower(strings.TrimSpace(*topology)))
	agentsByID := map[string]fleet.AgentRecord{}
	for _, a := range selected {
		agentsByID[a.ID] = a
	}

	results, runErr := scheduler.Execute(task, agentsByID, plan, scheduler.RunOptions{
		Topology:       *topology,
		TimeoutSeconds: *timeout,
		DryRun:         *dryRun,
	})

	fmt.Printf("fleet run task=%q selected=%d topology=%s dryRun=%t\n", task, len(selected), *topology, *dryRun)
	for i, a := range selected {
		fmt.Printf("  %d. %s (%s, %s)\n", i+1, a.Name, a.Protocol, a.Endpoint)
	}
	if *explain {
		sort.Strings(decisionLog)
		fmt.Println("selection decisions:")
		for _, line := range decisionLog {
			fmt.Println(line)
		}
	}
	if err := printFleetRunResults(results, *output); err != nil {
		return err
	}
	return runErr
}

func resolveInventoryPath(override string) (string, error) {
	v := strings.TrimSpace(override)
	if v != "" {
		return expandHome(v), nil
	}
	return inventory.DefaultPath()
}

func expandHome(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func parsePorts(v string) ([]int, error) {
	parts := csvToList(v)
	if len(parts) == 0 {
		return nil, errors.New("no ports provided")
	}
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return nil, fmt.Errorf("invalid port %q", p)
		}
		out = append(out, n)
	}
	return out, nil
}

func printFleetAgents(agents []fleet.AgentRecord, format string) error {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(agents, "", "  ")
		fmt.Println(string(b))
		return nil
	case "table":
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tHOST\tPROTO\tHEALTH\tLATENCY(ms)\tENDPOINT\tSEEN")
		for _, a := range agents {
			seen := ""
			if !a.LastSeenAt.IsZero() {
				seen = a.LastSeenAt.Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n", a.ID, a.Name, a.Host, a.Protocol, a.Health, a.LatencyMs, a.Endpoint, seen)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("unsupported output %q", format)
	}
}

func printFleetRunResults(results []scheduler.StepResult, format string) error {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(b))
		return nil
	case "table":
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "STEP\tAGENT_ID\tSTATUS\tDURATION(ms)\tERROR")
		for _, r := range results {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", r.Name, r.AgentID, r.Status, r.DurationMs, r.Error)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("unsupported output %q", format)
	}
}

func filterFleetAgents(in []fleet.AgentRecord, status, protocol, host string, skills []string) []fleet.AgentRecord {
	s := strings.ToLower(strings.TrimSpace(status))
	p := strings.ToLower(strings.TrimSpace(protocol))
	h := strings.TrimSpace(host)
	out := make([]fleet.AgentRecord, 0, len(in))
	for _, a := range in {
		if s != "" && s != "all" && a.Health != s {
			continue
		}
		if p != "" && p != "all" && a.Protocol != p {
			continue
		}
		if h != "" && a.Host != h {
			continue
		}
		if _, ok := fleetSkillMatch(a, skills); !ok {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func fleetSkillMatch(a fleet.AgentRecord, required []string) (int, bool) {
	if len(required) == 0 {
		return len(a.Skills), true
	}
	have := map[string]bool{}
	for _, s := range a.Skills {
		have[s] = true
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

func matchIDOrName(set map[string]bool, a fleet.AgentRecord) bool {
	if len(set) == 0 {
		return false
	}
	return set[a.ID] || set[a.Name]
}
