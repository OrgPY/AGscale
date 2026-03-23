package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"agscale/internal/fleet"
	"agscale/internal/fleet/discovery"
	"agscale/internal/fleet/inventory"
	"agscale/internal/fleet/scheduler"
	fleetstate "agscale/internal/fleet/state"
)

const fleetStateDirSuffix = ".agscale/fleet"

func runFleet(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fleet <agent|host|run|scan|list|inspect>")
	}
	switch args[0] {
	case "agent":
		return runFleetAgent(args[1:])
	case "host":
		return runFleetHost(args[1:])
	case "run":
		return runFleetRun(args[1:])
	case "scan":
		return runFleetScan(args[1:])
	case "list":
		return runFleetList(args[1:])
	case "inspect":
		return runFleetInspect(args[1:])
	default:
		return fmt.Errorf("unknown fleet subcommand %q", args[0])
	}
}

func runFleetAgent(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fleet agent <scan|list|inspect|chat|pin|unpin>")
	}
	switch args[0] {
	case "scan":
		return runFleetScan(args[1:])
	case "list":
		return runFleetList(args[1:])
	case "inspect":
		return runFleetInspect(args[1:])
	case "chat":
		return runFleetAgentChat(args[1:])
	case "pin":
		return runFleetAgentPin(args[1:])
	case "unpin":
		return runFleetAgentUnpin(args[1:])
	default:
		return fmt.Errorf("unknown fleet agent subcommand %q", args[0])
	}
}

func runFleetHost(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fleet host <add|list|inspect|label|rm|doctor>")
	}
	switch args[0] {
	case "add":
		return runFleetHostAdd(args[1:])
	case "list":
		return runFleetHostList(args[1:])
	case "inspect":
		return runFleetHostInspect(args[1:])
	case "label":
		return runFleetHostLabel(args[1:])
	case "rm":
		return runFleetHostRemove(args[1:])
	case "doctor":
		return runFleetHostDoctor(args[1:])
	default:
		return fmt.Errorf("unknown fleet host subcommand %q", args[0])
	}
}

func runFleetRun(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fleet run <dispatch|history|inspect> [args]")
	}
	switch args[0] {
	case "dispatch":
		return runFleetRunDispatch(args[1:])
	case "history":
		return runFleetRunHistory(args[1:])
	case "inspect":
		return runFleetRunInspect(args[1:])
	default:
		return runFleetRunDispatch(args)
	}
}

func runFleetScan(args []string) error {
	fs := flag.NewFlagSet("fleet agent scan", flag.ContinueOnError)
	local := fs.Bool("local", true, "scan localhost")
	hostsRaw := fs.String("hosts", "", "ssh hosts, comma-separated")
	sshCfg := fs.String("ssh-config", defaultFleetSSHConfig(), "ssh config path")
	portsRaw := fs.String("ports", defaultPortsCSV(), "port candidates")
	scanMode := fs.String("scan-mode", "agent-aware", "scan mode: agent-aware|ports")
	signaturesRaw := fs.String("signatures", strings.Join(discovery.DefaultAgentSignatures(), ","), "process signatures for --scan-mode agent-aware")
	timeout := fs.Int("timeout-seconds", 3, "probe timeout seconds")
	output := fs.String("output", "table", "output format: table|json")
	write := fs.Bool("write", false, "persist scan result to fleet state")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	stateDir, invPath, hostsPath, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	ports, err := parsePorts(*portsRaw)
	if err != nil {
		return err
	}
	opts := fleet.ProbeOptions{TimeoutSeconds: *timeout}
	found := []fleet.AgentRecord{}
	discoveredHosts := []fleet.HostRecord{}
	existingHosts, err := fleetstate.LoadHosts(hostsPath)
	if err != nil {
		return err
	}

	if *local {
		localFound, err := scanLocalFleetAgents(*scanMode, *signaturesRaw, ports, opts)
		if err != nil {
			return err
		}
		found = append(found, localFound...)
		discoveredHosts = append(discoveredHosts, localHostRecord(localFound))
	}

	remoteTargets := collectRemoteScanTargets(existingHosts, csvToList(*hostsRaw))
	if len(remoteTargets) > 0 {
		targetNames := make([]string, 0, len(remoteTargets))
		for _, host := range remoteTargets {
			targetNames = append(targetNames, sshTargetForHost(host))
		}
		remoteFound := discovery.ScanSSHHosts(targetNames, expandHome(*sshCfg), ports, opts)
		found = append(found, remoteFound...)
		discoveredHosts = append(discoveredHosts, summarizeDiscoveredHosts(remoteTargets, remoteFound)...)
	}

	found = dedupeFleetAgents(found)

	if *write {
		existing, err := inventory.Load(invPath)
		if err != nil {
			return err
		}
		merged := inventory.Merge(existing, found)
		if err := inventory.Save(invPath, merged); err != nil {
			return err
		}
		mergedHosts := fleetstate.MergeHosts(existingHosts, discoveredHosts)
		if err := fleetstate.SaveHosts(hostsPath, mergedHosts); err != nil {
			return err
		}
		fmt.Printf("fleet state saved: %s (agents=%d hosts=%d)\n", stateDir, len(merged.Agents), len(mergedHosts))
	}

	return printFleetAgents(found, *output)
}

func runFleetList(args []string) error {
	fs := flag.NewFlagSet("fleet agent list", flag.ContinueOnError)
	status := fs.String("status", "all", "healthy|degraded|unknown|all")
	protocol := fs.String("protocol", "all", "http|mcp|all")
	host := fs.String("host", "", "host filter")
	skillsRaw := fs.String("skills", "", "required skills s1,s2")
	output := fs.String("output", "table", "output format: table|json")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, invPath, _, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	agents, err := loadFleetAgents(invPath)
	if err != nil {
		return err
	}
	requiredSkills := csvToList(*skillsRaw)
	filtered := filterFleetAgents(agents, *status, *protocol, *host, requiredSkills)
	return printFleetAgents(filtered, *output)
}

func runFleetInspect(args []string) error {
	fs := flag.NewFlagSet("fleet agent inspect", flag.ContinueOnError)
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: fleet agent inspect <agent-id-or-name>")
	}
	_, invPath, _, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	agents, err := loadFleetAgents(invPath)
	if err != nil {
		return err
	}
	agent, ok := findFleetAgent(agents, strings.TrimSpace(rest[0]))
	if !ok {
		return fmt.Errorf("agent %q not found in inventory %s", rest[0], invPath)
	}
	return printJSON(agent)
}

func runFleetAgentChat(args []string) error {
	fs := flag.NewFlagSet("fleet agent chat", flag.ContinueOnError)
	message := fs.String("message", "", "single prompt to send (omit for interactive chat-first mode)")
	raw := fs.Bool("raw", false, "print raw JSON response")
	sshCfg := fs.String("ssh-config", defaultFleetSSHConfig(), "ssh config path")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	agentTarget, parseArgs := extractLeadingTargetArg(args)
	if err := fs.Parse(parseArgs); err != nil {
		return err
	}
	rest := prependTargetArg(agentTarget, fs.Args())
	if len(rest) < 1 {
		return errors.New("usage: fleet agent chat <agent-id-or-name> [--message 'hello'] [--ssh-config ~/.ssh/config]")
	}
	_, invPath, _, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	agents, err := loadFleetAgents(invPath)
	if err != nil {
		return err
	}
	agent, ok := findFleetAgent(agents, strings.TrimSpace(rest[0]))
	if !ok {
		return fmt.Errorf("agent %q not found in inventory %s", rest[0], invPath)
	}
	chatTarget := newFleetTUITarget(agent)
	chatTarget.SSHConfigPath = expandHome(*sshCfg)
	if strings.TrimSpace(*message) == "" {
		return runInventoryAgentTUIChatFirst(chatTarget, invPath)
	}

	pf := &tuiPortForwardSession{}
	defer pf.close()
	resp, err := sendTUIChatMessage(chatTarget, *message, pf)
	if err != nil {
		return err
	}
	if *raw {
		fmt.Println(resp.raw)
		return nil
	}
	fmt.Println(resp.text)
	return nil
}

func runFleetAgentPin(args []string) error {
	fs := flag.NewFlagSet("fleet agent pin", flag.ContinueOnError)
	endpoint := fs.String("endpoint", "", "explicit agent base endpoint")
	reachability := fs.String("reachability", "", "direct|ssh-loopback (default inferred from host and endpoint)")
	protocol := fs.String("protocol", "", "override protocol: http|mcp")
	chatPath := fs.String("chat-path", "", "override chat path, for example /chat or /v1/chat/completions")
	model := fs.String("model", "", "override default model metadata")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	target, parseArgs := extractLeadingTargetArg(args)
	if err := fs.Parse(parseArgs); err != nil {
		return err
	}
	rest := prependTargetArg(target, fs.Args())
	if len(rest) < 1 {
		return errors.New("usage: fleet agent pin <agent-id-or-name> --endpoint <url> [--reachability direct|ssh-loopback] [--protocol http|mcp] [--chat-path /chat] [--model name]")
	}
	if strings.TrimSpace(*endpoint) == "" {
		return errors.New("--endpoint is required")
	}
	if strings.TrimSpace(*protocol) != "" {
		p := strings.ToLower(strings.TrimSpace(*protocol))
		if p != "http" && p != "mcp" {
			return fmt.Errorf("unsupported protocol %q (use http|mcp)", *protocol)
		}
	}
	if strings.TrimSpace(*reachability) != "" && normalizeReachability(*reachability) == "" {
		return fmt.Errorf("unsupported reachability %q (use direct|ssh-loopback)", *reachability)
	}

	_, invPath, _, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	inv, err := inventory.Load(invPath)
	if err != nil {
		return err
	}
	idx := findFleetAgentIndex(inv.Agents, strings.TrimSpace(rest[0]))
	if idx < 0 {
		return fmt.Errorf("agent %q not found in inventory %s", rest[0], invPath)
	}

	agent := fleet.NormalizeAgentRecord(inv.Agents[idx])
	agent = ensureDiscoveredFallback(agent)
	agent.Endpoint = normalizeAgentEndpoint(*endpoint)
	if strings.TrimSpace(*protocol) != "" {
		agent.Protocol = strings.ToLower(strings.TrimSpace(*protocol))
	}
	agent.EndpointSource = fleet.EndpointSourcePinned
	agent.Reachability = normalizeReachability(*reachability)
	if agent.Reachability == "" {
		agent.Reachability = fleet.InferAgentReachability(agent)
	}
	if agent.Metadata == nil {
		agent.Metadata = map[string]string{}
	}
	if strings.TrimSpace(*chatPath) != "" {
		agent.Metadata[fleet.MetadataChatPath] = normalizeChatPath(*chatPath)
	}
	if strings.TrimSpace(*model) != "" {
		agent.Model = strings.TrimSpace(*model)
		agent.Metadata[fleet.MetadataModel] = agent.Model
	}
	inv.Agents[idx] = fleet.NormalizeAgentRecord(agent)
	if err := inventory.Save(invPath, inv); err != nil {
		return err
	}
	fmt.Printf("agent pinned: %s -> %s (%s)\n", agent.Name, agent.Endpoint, agent.Reachability)
	return nil
}

func runFleetAgentUnpin(args []string) error {
	fs := flag.NewFlagSet("fleet agent unpin", flag.ContinueOnError)
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	target, parseArgs := extractLeadingTargetArg(args)
	if err := fs.Parse(parseArgs); err != nil {
		return err
	}
	rest := prependTargetArg(target, fs.Args())
	if len(rest) < 1 {
		return errors.New("usage: fleet agent unpin <agent-id-or-name>")
	}

	_, invPath, _, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	inv, err := inventory.Load(invPath)
	if err != nil {
		return err
	}
	idx := findFleetAgentIndex(inv.Agents, strings.TrimSpace(rest[0]))
	if idx < 0 {
		return fmt.Errorf("agent %q not found in inventory %s", rest[0], invPath)
	}

	agent := fleet.NormalizeAgentRecord(inv.Agents[idx])
	agent.EndpointSource = fleet.EndpointSourceDiscovered
	if agent.Metadata != nil {
		if v := strings.TrimSpace(agent.Metadata[fleet.MetadataDiscoveredEndpoint]); v != "" {
			agent.Endpoint = v
		}
		if v := strings.TrimSpace(agent.Metadata[fleet.MetadataDiscoveredProtocol]); v != "" {
			agent.Protocol = v
		}
		if v := normalizeReachability(agent.Metadata[fleet.MetadataDiscoveredReachability]); v != "" {
			agent.Reachability = v
		} else {
			agent.Reachability = fleet.InferAgentReachability(agent)
		}
		if v := strings.TrimSpace(agent.Metadata[fleet.MetadataDiscoveredChatPath]); v != "" {
			agent.Metadata[fleet.MetadataChatPath] = v
		}
	}
	agent = fleet.NormalizeAgentRecord(agent)
	inv.Agents[idx] = agent
	if err := inventory.Save(invPath, inv); err != nil {
		return err
	}
	fmt.Printf("agent unpinned: %s -> %s (%s)\n", agent.Name, agent.Endpoint, agent.Reachability)
	return nil
}

func runFleetHostAdd(args []string) error {
	fs := flag.NewFlagSet("fleet host add", flag.ContinueOnError)
	address := fs.String("address", "", "ssh hostname, IP, or SSH config alias")
	sshProfile := fs.String("ssh-profile", "", "ssh profile or alias override")
	labelsRaw := fs.String("labels", "", "labels k=v,k2=v2")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: fleet host add <name> [--address host-or-alias] [--ssh-profile profile] [--labels k=v]")
	}
	labels, err := parseSimpleLabelSelector(*labelsRaw)
	if err != nil {
		return err
	}
	stateDir, _, hostsPath, _, err := resolveFleetPaths(*stateDirFlag, "")
	if err != nil {
		return err
	}
	hosts, err := fleetstate.LoadHosts(hostsPath)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(rest[0])
	addr := strings.TrimSpace(*address)
	if addr == "" {
		addr = name
	}
	host := fleet.HostRecord{
		ID:         fleet.StableHostID(addr),
		Name:       name,
		Address:    addr,
		SSHProfile: strings.TrimSpace(*sshProfile),
		Source:     "manual",
		Health:     fleet.HealthUnknown,
		Labels:     labels,
	}
	hosts = fleetstate.MergeHosts(hosts, []fleet.HostRecord{host})
	if err := fleetstate.SaveHosts(hostsPath, hosts); err != nil {
		return err
	}
	fmt.Printf("host saved: %s (%s)\n", name, stateDir)
	return nil
}

func runFleetHostList(args []string) error {
	fs := flag.NewFlagSet("fleet host list", flag.ContinueOnError)
	status := fs.String("status", "all", "healthy|degraded|unknown|all")
	output := fs.String("output", "table", "output format: table|json")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	stateDir, invPath, _, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	hosts, agents, err := loadFleetHosts(stateDir, invPath)
	if err != nil {
		return err
	}
	filtered := filterFleetHosts(hosts, *status)
	return printFleetHosts(filtered, countAgentsByHostID(agents), *output)
}

func runFleetHostInspect(args []string) error {
	fs := flag.NewFlagSet("fleet host inspect", flag.ContinueOnError)
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: fleet host inspect <host-id-or-name>")
	}
	stateDir, invPath, _, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	hosts, _, err := loadFleetHosts(stateDir, invPath)
	if err != nil {
		return err
	}
	host, _, ok := fleetstate.FindHost(hosts, strings.TrimSpace(rest[0]))
	if !ok {
		return fmt.Errorf("host %q not found", rest[0])
	}
	return printJSON(host)
}

func runFleetHostLabel(args []string) error {
	fs := flag.NewFlagSet("fleet host label", flag.ContinueOnError)
	setRaw := fs.String("set", "", "labels to merge k=v,k2=v2")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: fleet host label <host-id-or-name> --set k=v,k2=v2")
	}
	if strings.TrimSpace(*setRaw) == "" {
		return errors.New("--set is required")
	}
	labels, err := parseSimpleLabelSelector(*setRaw)
	if err != nil {
		return err
	}
	_, _, hostsPath, _, err := resolveFleetPaths(*stateDirFlag, "")
	if err != nil {
		return err
	}
	hosts, err := fleetstate.LoadHosts(hostsPath)
	if err != nil {
		return err
	}
	host, idx, ok := fleetstate.FindHost(hosts, strings.TrimSpace(rest[0]))
	if !ok {
		return fmt.Errorf("host %q not found", rest[0])
	}
	if host.Labels == nil {
		host.Labels = map[string]string{}
	}
	for k, v := range labels {
		host.Labels[k] = v
	}
	hosts[idx] = host
	return fleetstate.SaveHosts(hostsPath, hosts)
}

func runFleetHostRemove(args []string) error {
	fs := flag.NewFlagSet("fleet host rm", flag.ContinueOnError)
	keepAgents := fs.Bool("keep-agents", false, "leave associated agents in inventory")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: fleet host rm <host-id-or-name> [--keep-agents]")
	}
	_, invPath, hostsPath, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	hosts, err := fleetstate.LoadHosts(hostsPath)
	if err != nil {
		return err
	}
	hosts, removed, ok := fleetstate.RemoveHost(hosts, strings.TrimSpace(rest[0]))
	if !ok {
		return fmt.Errorf("host %q not found", rest[0])
	}
	if err := fleetstate.SaveHosts(hostsPath, hosts); err != nil {
		return err
	}
	if !*keepAgents {
		inv, err := inventory.Load(invPath)
		if err != nil {
			return err
		}
		filtered := make([]fleet.AgentRecord, 0, len(inv.Agents))
		for _, agent := range inv.Agents {
			if agent.HostID == removed.ID || agent.Host == removed.Name || agent.Host == removed.Address {
				continue
			}
			filtered = append(filtered, agent)
		}
		inv.Agents = filtered
		if err := inventory.Save(invPath, inv); err != nil {
			return err
		}
	}
	fmt.Printf("host removed: %s\n", removed.Name)
	return nil
}

func runFleetHostDoctor(args []string) error {
	fs := flag.NewFlagSet("fleet host doctor", flag.ContinueOnError)
	sshCfg := fs.String("ssh-config", defaultFleetSSHConfig(), "ssh config path")
	timeout := fs.Int("timeout-seconds", 5, "ssh timeout seconds")
	write := fs.Bool("write", false, "persist refreshed health to fleet state")
	output := fs.String("output", "table", "output format: table|json")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	stateDir, invPath, hostsPath, _, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	hosts, agents, err := loadFleetHosts(stateDir, invPath)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		host, _, ok := fleetstate.FindHost(hosts, strings.TrimSpace(rest[0]))
		if !ok {
			return fmt.Errorf("host %q not found", rest[0])
		}
		hosts = []fleet.HostRecord{host}
	}

	checked := make([]fleet.HostRecord, 0, len(hosts))
	for _, host := range hosts {
		host.LastSeenAt = time.Now().UTC()
		if isLocalHost(host) {
			host.Health = fleet.HealthHealthy
			host.LastError = ""
			host.Resources = mergeResourceSnapshots(host.Resources, localResourceSnapshot(agents))
			checked = append(checked, host)
			continue
		}
		if err := discovery.CheckSSHHost(sshTargetForHost(host), expandHome(*sshCfg), *timeout); err != nil {
			host.Health = fleet.HealthDegraded
			host.LastError = err.Error()
		} else {
			host.Health = fleet.HealthHealthy
			host.LastError = ""
		}
		checked = append(checked, host)
	}

	if *write {
		existing, err := fleetstate.LoadHosts(hostsPath)
		if err != nil {
			return err
		}
		if err := fleetstate.SaveHosts(hostsPath, fleetstate.MergeHosts(existing, checked)); err != nil {
			return err
		}
	}

	return printFleetHosts(checked, countAgentsByHostID(agents), *output)
}

func runFleetRunDispatch(args []string) error {
	fs := flag.NewFlagSet("fleet run dispatch", flag.ContinueOnError)
	agentsRaw := fs.String("agents", "", "explicit include list by id or name")
	excludeRaw := fs.String("exclude-agents", "", "exclude list by id or name")
	selectorRaw := fs.String("selector", "", "label selector k=v,k2=v2")
	requiredSkillsRaw := fs.String("require-skills", "", "required skills s1,s2")
	maxAgents := fs.Int("max-agents", 3, "max selected agents")
	topology := fs.String("topology", "dag", "dag|sequential")
	explain := fs.Bool("explain-selection", false, "print why each agent selected/skipped")
	dryRun := fs.Bool("dry-run", false, "print plan only, do not invoke endpoints")
	timeout := fs.Int("timeout-seconds", 20, "step invoke timeout")
	sshCfg := fs.String("ssh-config", defaultFleetSSHConfig(), "ssh config path")
	output := fs.String("output", "table", "output format: table|json")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	path := fs.String("inventory", "", "inventory path override (default <state-dir>/agents.json)")
	task, parseArgs := extractLeadingTargetArg(args)
	if err := fs.Parse(parseArgs); err != nil {
		return err
	}
	rest := prependTargetArg(task, fs.Args())
	if len(rest) < 1 {
		return errors.New("usage: fleet run [dispatch] <task-name> [--agents a,b] [--exclude-agents x,y] [--selector k=v] [--require-skills s1,s2] [--max-agents 3] [--topology dag|sequential] [--explain-selection] [--dry-run] [--ssh-config ~/.ssh/config]")
	}
	if *maxAgents <= 0 {
		return errors.New("--max-agents must be > 0")
	}
	stateDir, invPath, _, runsPath, err := resolveFleetPaths(*stateDirFlag, *path)
	if err != nil {
		return err
	}
	agents, err := loadFleetAgents(invPath)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		return fmt.Errorf("inventory empty: %s (run 'agscale fleet agent scan --local --write' first)", invPath)
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
	for _, agent := range agents {
		reasons := []string{}
		if len(includeSet) > 0 && !matchIDOrName(includeSet, agent) {
			reasons = append(reasons, "not in --agents allowlist")
		}
		if matchIDOrName(excludeSet, agent) {
			reasons = append(reasons, "matched --exclude-agents")
		}
		if len(reasons) == 0 && agent.Health != fleet.HealthHealthy {
			reasons = append(reasons, "agent not healthy")
		}
		if len(reasons) == 0 && !matchLabels(agent.Labels, labelSelector) {
			reasons = append(reasons, "label selector mismatch")
		}
		if len(reasons) == 0 {
			if _, ok := fleetSkillMatch(agent, requiredSkills); !ok {
				reasons = append(reasons, "missing required skills")
			}
		}
		if len(reasons) > 0 {
			if *explain {
				decisionLog = append(decisionLog, fmt.Sprintf("- %s: dropped (%s)", agent.Name, strings.Join(reasons, "; ")))
			}
			continue
		}
		if *explain {
			decisionLog = append(decisionLog, fmt.Sprintf("- %s: eligible (host=%s proto=%s %s)", agent.Name, firstNonEmpty(agent.Host, agent.HostID), agent.Protocol, agent.Endpoint))
		}
		candidates = append(candidates, agent)
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
	if *dryRun {
		for _, agent := range selected {
			agentsByID[agent.ID] = agent
		}
	} else {
		tunnels := newFleetTunnelManager(*sshCfg)
		defer tunnels.Close()
		for _, agent := range selected {
			prepared, err := tunnels.PrepareAgent(agent)
			if err != nil {
				return err
			}
			agentsByID[agent.ID] = prepared
		}
	}

	startedAt := time.Now().UTC()
	results, runErr := scheduler.Execute(rest[0], agentsByID, plan, scheduler.RunOptions{
		Topology:       *topology,
		TimeoutSeconds: *timeout,
		DryRun:         *dryRun,
	})
	record := buildFleetRunRecord(rest[0], *topology, *dryRun, *selectorRaw, requiredSkills, selected, results, startedAt, runErr)
	if err := fleetstate.AppendRun(runsPath, record, 200); err != nil {
		return err
	}

	fmt.Printf("fleet run task=%q selected=%d topology=%s dryRun=%t state=%s\n", rest[0], len(selected), *topology, *dryRun, stateDir)
	for i, agent := range selected {
		fmt.Printf("  %d. %s (%s, %s, host=%s)\n", i+1, agent.Name, agent.Protocol, agent.Endpoint, firstNonEmpty(agent.Host, agent.HostID))
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
	if runErr == nil {
		fmt.Printf("run recorded: %s\n", record.ID)
	}
	return runErr
}

func runFleetRunHistory(args []string) error {
	fs := flag.NewFlagSet("fleet run history", flag.ContinueOnError)
	limit := fs.Int("limit", 20, "maximum runs to print")
	output := fs.String("output", "table", "output format: table|json")
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	stateDir, err := resolveFleetStateDir(*stateDirFlag)
	if err != nil {
		return err
	}
	runs, err := fleetstate.LoadRuns(fleetRunsPath(stateDir))
	if err != nil {
		return err
	}
	if *limit > 0 && len(runs) > *limit {
		runs = runs[:*limit]
	}
	return printFleetRuns(runs, *output)
}

func runFleetRunInspect(args []string) error {
	fs := flag.NewFlagSet("fleet run inspect", flag.ContinueOnError)
	stateDirFlag := fs.String("state-dir", "", "fleet state dir (default ~/.agscale/fleet)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: fleet run inspect <run-id>")
	}
	stateDir, err := resolveFleetStateDir(*stateDirFlag)
	if err != nil {
		return err
	}
	runs, err := fleetstate.LoadRuns(fleetRunsPath(stateDir))
	if err != nil {
		return err
	}
	run, ok := fleetstate.FindRun(runs, strings.TrimSpace(rest[0]))
	if !ok {
		return fmt.Errorf("run %q not found", rest[0])
	}
	return printJSON(run)
}

func resolveInventoryPath(override string) (string, error) {
	v := strings.TrimSpace(override)
	if v != "" {
		return expandHome(v), nil
	}
	return inventory.DefaultPath()
}

func resolveFleetPaths(stateDirOverride, inventoryOverride string) (stateDir, inventoryPath, hostsPath, runsPath string, err error) {
	stateDir, err = resolveFleetStateDir(stateDirOverride)
	if err != nil {
		return "", "", "", "", err
	}
	if strings.TrimSpace(inventoryOverride) != "" {
		inventoryPath = expandHome(inventoryOverride)
	} else {
		inventoryPath = filepath.Join(stateDir, "agents.json")
	}
	return stateDir, inventoryPath, fleetHostsPath(stateDir), fleetRunsPath(stateDir), nil
}

func resolveFleetStateDir(override string) (string, error) {
	v := strings.TrimSpace(override)
	if v != "" {
		return expandHome(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fleetStateDirSuffix), nil
}

func fleetHostsPath(stateDir string) string {
	return filepath.Join(stateDir, "hosts.json")
}

func fleetRunsPath(stateDir string) string {
	return filepath.Join(stateDir, "runs.json")
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

func scanLocalFleetAgents(scanMode string, signaturesRaw string, ports []int, opts fleet.ProbeOptions) ([]fleet.AgentRecord, error) {
	mode := strings.ToLower(strings.TrimSpace(scanMode))
	switch mode {
	case "agent-aware":
		sigs := csvToList(signaturesRaw)
		if env := strings.TrimSpace(os.Getenv("AGSCALE_AGENT_SIGNATURES")); env != "" {
			sigs = append(sigs, csvToList(env)...)
		}
		localFound := discovery.ScanLocalAgentAware(opts, ports, sigs)
		if len(localFound) == 0 {
			endpoints := discovery.LocalDefaultEndpoints()
			if env := strings.TrimSpace(os.Getenv("AGSCALE_DISCOVERY_ENDPOINTS")); env != "" {
				endpoints = append(endpoints, csvToList(env)...)
			}
			localFound = discovery.ScanLocal(endpoints, opts)
		}
		return localFound, nil
	case "ports":
		endpoints := discovery.LocalDefaultEndpoints()
		if env := strings.TrimSpace(os.Getenv("AGSCALE_DISCOVERY_ENDPOINTS")); env != "" {
			endpoints = append(endpoints, csvToList(env)...)
		}
		return discovery.ScanLocal(endpoints, opts), nil
	default:
		return nil, fmt.Errorf("unsupported --scan-mode %q (use agent-aware|ports)", scanMode)
	}
}

func dedupeFleetAgents(found []fleet.AgentRecord) []fleet.AgentRecord {
	uniq := map[string]fleet.AgentRecord{}
	for _, agent := range found {
		uniq[agent.ID] = agent
	}
	out := make([]fleet.AgentRecord, 0, len(uniq))
	for _, agent := range uniq {
		out = append(out, agent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func collectRemoteScanTargets(existing []fleet.HostRecord, explicitHosts []string) []fleet.HostRecord {
	byID := map[string]fleet.HostRecord{}
	for _, host := range existing {
		if isLocalHost(host) {
			continue
		}
		id := strings.TrimSpace(host.ID)
		if id == "" {
			id = fleet.StableHostID(firstNonEmpty(host.Address, host.Name))
			host.ID = id
		}
		byID[id] = host
	}
	for _, explicit := range explicitHosts {
		target := strings.TrimSpace(explicit)
		if target == "" {
			continue
		}
		id := fleet.StableHostID(target)
		host := byID[id]
		if host.ID == "" {
			host = fleet.HostRecord{
				ID:      id,
				Name:    target,
				Address: target,
				Source:  "manual",
				Health:  fleet.HealthUnknown,
			}
		}
		if host.Address == "" {
			host.Address = target
		}
		if host.Name == "" {
			host.Name = target
		}
		byID[id] = host
	}
	out := make([]fleet.HostRecord, 0, len(byID))
	for _, host := range byID {
		out = append(out, host)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func localHostRecord(agents []fleet.AgentRecord) fleet.HostRecord {
	now := time.Now().UTC()
	return fleet.HostRecord{
		ID:         fleet.StableHostID("local"),
		Name:       "local",
		Address:    "local",
		Source:     "local-scan",
		Health:     fleet.HealthHealthy,
		LastSeenAt: now,
		Labels: map[string]string{
			"scope": "local",
		},
		Resources: localResourceSnapshot(agents),
	}
}

func summarizeDiscoveredHosts(targets []fleet.HostRecord, agents []fleet.AgentRecord) []fleet.HostRecord {
	agentsByHost := map[string][]fleet.AgentRecord{}
	for _, agent := range agents {
		agentsByHost[agent.HostID] = append(agentsByHost[agent.HostID], agent)
	}
	out := make([]fleet.HostRecord, 0, len(targets))
	for _, host := range targets {
		host.ID = firstNonEmpty(host.ID, fleet.StableHostID(firstNonEmpty(host.Address, host.Name)))
		host.LastSeenAt = time.Now().UTC()
		host.Source = firstNonEmpty(host.Source, "ssh-scan")
		hostResources := summarizeHostResources(agentsByHost[host.ID])
		host.Resources = mergeResourceSnapshots(host.Resources, hostResources)
		discovered := agentsByHost[host.ID]
		switch {
		case len(discovered) == 0:
			host.Health = fleet.HealthUnknown
			host.LastError = "no agents discovered in latest scan"
		case anyHealthyAgents(discovered):
			host.Health = fleet.HealthHealthy
			host.LastError = ""
		default:
			host.Health = fleet.HealthDegraded
			host.LastError = "all discovered agents are degraded"
		}
		out = append(out, host)
	}
	return out
}

func summarizeHostResources(agents []fleet.AgentRecord) fleet.ResourceSnapshot {
	resource := fleet.ResourceSnapshot{}
	ports := map[int]bool{}
	models := map[string]bool{}
	for _, agent := range agents {
		for _, port := range agent.Resources.Ports {
			if port > 0 {
				ports[port] = true
			}
		}
		for _, model := range agent.Resources.Models {
			model = strings.TrimSpace(model)
			if model != "" {
				models[model] = true
			}
		}
		if model := strings.TrimSpace(agent.Model); model != "" {
			models[model] = true
		}
	}
	resource.Ports = sortedPortList(ports)
	resource.Models = sortedStringSet(models)
	return resource
}

func localResourceSnapshot(agents []fleet.AgentRecord) fleet.ResourceSnapshot {
	resource := summarizeHostResources(agents)
	resource.CPUCores = runtime.NumCPU()
	return resource
}

func mergeResourceSnapshots(current, update fleet.ResourceSnapshot) fleet.ResourceSnapshot {
	if update.CPUCores == 0 {
		update.CPUCores = current.CPUCores
	}
	if update.MemoryMiB == 0 {
		update.MemoryMiB = current.MemoryMiB
	}
	if update.GPUCount == 0 {
		update.GPUCount = current.GPUCount
	}
	if update.DiskFreeMiB == 0 {
		update.DiskFreeMiB = current.DiskFreeMiB
	}
	if len(update.Ports) == 0 {
		update.Ports = current.Ports
	}
	if len(update.Models) == 0 {
		update.Models = current.Models
	}
	return update
}

func anyHealthyAgents(agents []fleet.AgentRecord) bool {
	for _, agent := range agents {
		if agent.Health == fleet.HealthHealthy {
			return true
		}
	}
	return false
}

func sortedPortList(set map[int]bool) []int {
	if len(set) == 0 {
		return nil
	}
	out := make([]int, 0, len(set))
	for port := range set {
		out = append(out, port)
	}
	sort.Ints(out)
	return out
}

func sortedStringSet(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func loadFleetAgents(inventoryPath string) ([]fleet.AgentRecord, error) {
	inv, err := inventory.Load(inventoryPath)
	if err != nil {
		return nil, err
	}
	for i := range inv.Agents {
		inv.Agents[i] = fleet.NormalizeAgentRecord(inv.Agents[i])
	}
	return inv.Agents, nil
}

func loadFleetHosts(stateDir, inventoryPath string) ([]fleet.HostRecord, []fleet.AgentRecord, error) {
	agents, err := loadFleetAgents(inventoryPath)
	if err != nil {
		return nil, nil, err
	}
	hosts, err := fleetstate.LoadHosts(fleetHostsPath(stateDir))
	if err != nil {
		return nil, nil, err
	}
	return fleetstate.MergeHosts(hosts, deriveHostsFromAgents(agents)), agents, nil
}

func deriveHostsFromAgents(agents []fleet.AgentRecord) []fleet.HostRecord {
	byID := map[string]fleet.HostRecord{}
	agentsByHost := map[string][]fleet.AgentRecord{}
	for _, agent := range agents {
		hostKey := strings.TrimSpace(agent.HostID)
		if hostKey == "" {
			hostKey = fleet.StableHostID(firstNonEmpty(agent.Host, "local"))
		}
		agentsByHost[hostKey] = append(agentsByHost[hostKey], agent)
		host := byID[hostKey]
		host.ID = hostKey
		host.Name = firstNonEmpty(host.Name, agent.Host, hostKey)
		host.Address = firstNonEmpty(host.Address, agent.Host)
		host.Source = firstNonEmpty(host.Source, agent.Source)
		host.Health = bestHostHealth(host.Health, agent.Health)
		host.LastSeenAt = maxTime(host.LastSeenAt, agent.LastSeenAt)
		host.Labels = mergeStringMaps(host.Labels, map[string]string{"scope": agent.Labels["scope"]})
		host.Resources = mergeResourceSnapshots(host.Resources, summarizeHostResources(agentsByHost[hostKey]))
		if host.LastError == "" {
			host.LastError = agent.LastError
		}
		byID[hostKey] = host
	}
	out := make([]fleet.HostRecord, 0, len(byID))
	for _, host := range byID {
		if isLocalHost(host) {
			host.Resources = mergeResourceSnapshots(host.Resources, localResourceSnapshot(agentsByHost[host.ID]))
		}
		out = append(out, host)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func bestHostHealth(current, next string) string {
	if current == fleet.HealthHealthy || next == fleet.HealthHealthy {
		return fleet.HealthHealthy
	}
	if current == fleet.HealthDegraded || next == fleet.HealthDegraded {
		return fleet.HealthDegraded
	}
	return firstNonEmpty(next, current, fleet.HealthUnknown)
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func mergeStringMaps(base map[string]string, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range base {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	for k, v := range extra {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sshTargetForHost(host fleet.HostRecord) string {
	return firstNonEmpty(host.SSHProfile, host.Address, host.Name)
}

func isLocalHost(host fleet.HostRecord) bool {
	value := strings.TrimSpace(firstNonEmpty(host.Address, host.Name))
	return value == "" || value == "local" || value == "127.0.0.1" || value == "localhost"
}

func findFleetAgent(agents []fleet.AgentRecord, target string) (fleet.AgentRecord, bool) {
	for _, agent := range agents {
		if agent.ID == target || agent.Name == target {
			return fleet.NormalizeAgentRecord(agent), true
		}
	}
	return fleet.AgentRecord{}, false
}

func findFleetAgentIndex(agents []fleet.AgentRecord, target string) int {
	for i, agent := range agents {
		if agent.ID == target || agent.Name == target {
			return i
		}
	}
	return -1
}

func ensureDiscoveredFallback(agent fleet.AgentRecord) fleet.AgentRecord {
	agent = fleet.NormalizeAgentRecord(agent)
	if agent.Metadata == nil {
		agent.Metadata = map[string]string{}
	}
	if strings.TrimSpace(agent.Metadata[fleet.MetadataDiscoveredEndpoint]) == "" {
		agent.Metadata[fleet.MetadataDiscoveredEndpoint] = strings.TrimSpace(agent.Endpoint)
	}
	if strings.TrimSpace(agent.Metadata[fleet.MetadataDiscoveredProtocol]) == "" {
		agent.Metadata[fleet.MetadataDiscoveredProtocol] = strings.TrimSpace(agent.Protocol)
	}
	if strings.TrimSpace(agent.Metadata[fleet.MetadataDiscoveredReachability]) == "" {
		agent.Metadata[fleet.MetadataDiscoveredReachability] = strings.TrimSpace(agent.Reachability)
	}
	if chatPath := strings.TrimSpace(agent.Metadata[fleet.MetadataChatPath]); chatPath != "" && strings.TrimSpace(agent.Metadata[fleet.MetadataDiscoveredChatPath]) == "" {
		agent.Metadata[fleet.MetadataDiscoveredChatPath] = chatPath
	}
	return agent
}

func normalizeReachability(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return ""
	case fleet.ReachabilityDirect:
		return fleet.ReachabilityDirect
	case fleet.ReachabilitySSHLoopback:
		return fleet.ReachabilitySSHLoopback
	default:
		return ""
	}
}

func normalizeAgentEndpoint(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if !strings.Contains(v, "://") {
		v = "http://" + v
	}
	return strings.TrimSuffix(v, "/")
}

func normalizeChatPath(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "/") {
		return v
	}
	return "/" + v
}

func buildFleetRunRecord(task, topology string, dryRun bool, selector string, requiredSkills []string, selected []fleet.AgentRecord, results []scheduler.StepResult, startedAt time.Time, runErr error) fleet.RunRecord {
	record := fleet.RunRecord{
		ID:             fmt.Sprintf("%s-%d", sanitizeName(task), startedAt.Unix()),
		Task:           task,
		Status:         "Succeeded",
		Topology:       topology,
		DryRun:         dryRun,
		Selector:       strings.TrimSpace(selector),
		RequiredSkills: requiredSkills,
		SelectedAgents: make([]string, 0, len(selected)),
		StartedAt:      startedAt,
		CompletedAt:    time.Now().UTC(),
		Results:        make([]fleet.RunStepResult, 0, len(results)),
	}
	if runErr != nil {
		record.Status = "Failed"
		record.Error = runErr.Error()
	}
	for _, agent := range selected {
		record.SelectedAgents = append(record.SelectedAgents, firstNonEmpty(agent.Name, agent.ID))
	}
	for _, result := range results {
		record.Results = append(record.Results, fleet.RunStepResult{
			Name:       result.Name,
			AgentID:    result.AgentID,
			Status:     result.Status,
			DurationMs: result.DurationMs,
			Error:      result.Error,
			Output:     result.Output,
		})
	}
	return record
}

func printFleetAgents(agents []fleet.AgentRecord, format string) error {
	switch format {
	case "json":
		return printJSON(agents)
	case "table":
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tHOST\tENGINE\tMODEL\tPROTO\tSOURCE\tREACH\tHEALTH\tENDPOINT\tSEEN")
		for _, agent := range agents {
			agent = fleet.NormalizeAgentRecord(agent)
			seen := ""
			if !agent.LastSeenAt.IsZero() {
				seen = agent.LastSeenAt.Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				agent.ID,
				agent.Name,
				firstNonEmpty(agent.Host, agent.HostID),
				agent.Engine,
				agent.Model,
				agent.Protocol,
				agent.EndpointSource,
				agent.Reachability,
				agent.Health,
				agent.Endpoint,
				seen,
			)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("unsupported output %q", format)
	}
}

func printFleetHosts(hosts []fleet.HostRecord, counts map[string]int, format string) error {
	switch format {
	case "json":
		return printJSON(hosts)
	case "table":
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tADDRESS\tHEALTH\tAGENTS\tMODELS\tPORTS\tSEEN")
		for _, host := range hosts {
			seen := ""
			if !host.LastSeenAt.IsZero() {
				seen = host.LastSeenAt.Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
				host.ID,
				host.Name,
				firstNonEmpty(host.Address, host.Name),
				host.Health,
				counts[host.ID],
				strings.Join(host.Resources.Models, ","),
				joinPorts(host.Resources.Ports),
				seen,
			)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("unsupported output %q", format)
	}
}

func printFleetRunResults(results []scheduler.StepResult, format string) error {
	switch format {
	case "json":
		return printJSON(results)
	case "table":
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "STEP\tAGENT_ID\tSTATUS\tDURATION(ms)\tERROR")
		for _, result := range results {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", result.Name, result.AgentID, result.Status, result.DurationMs, result.Error)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("unsupported output %q", format)
	}
}

func printFleetRuns(runs []fleet.RunRecord, format string) error {
	switch format {
	case "json":
		return printJSON(runs)
	case "table":
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tTASK\tSTATUS\tTOPOLOGY\tAGENTS\tSTARTED\tCOMPLETED")
		for _, run := range runs {
			completed := ""
			if !run.CompletedAt.IsZero() {
				completed = run.CompletedAt.Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				run.ID,
				run.Task,
				run.Status,
				run.Topology,
				strings.Join(run.SelectedAgents, ","),
				run.StartedAt.Format(time.RFC3339),
				completed,
			)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("unsupported output %q", format)
	}
}

func joinPorts(ports []int) string {
	if len(ports) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, strconv.Itoa(port))
	}
	return strings.Join(parts, ",")
}

func defaultPortsCSV() string {
	ports := discovery.DefaultAgentPorts()
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, strconv.Itoa(port))
	}
	return strings.Join(parts, ",")
}

func countAgentsByHostID(agents []fleet.AgentRecord) map[string]int {
	counts := map[string]int{}
	for _, agent := range agents {
		hostKey := strings.TrimSpace(agent.HostID)
		if hostKey == "" {
			hostKey = fleet.StableHostID(firstNonEmpty(agent.Host, "local"))
		}
		counts[hostKey]++
	}
	return counts
}

func filterFleetHosts(hosts []fleet.HostRecord, status string) []fleet.HostRecord {
	want := strings.ToLower(strings.TrimSpace(status))
	out := make([]fleet.HostRecord, 0, len(hosts))
	for _, host := range hosts {
		if want != "" && want != "all" && host.Health != want {
			continue
		}
		out = append(out, host)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func filterFleetAgents(in []fleet.AgentRecord, status, protocol, host string, skills []string) []fleet.AgentRecord {
	s := strings.ToLower(strings.TrimSpace(status))
	p := strings.ToLower(strings.TrimSpace(protocol))
	h := strings.TrimSpace(host)
	out := make([]fleet.AgentRecord, 0, len(in))
	for _, agent := range in {
		agent = fleet.NormalizeAgentRecord(agent)
		if s != "" && s != "all" && agent.Health != s {
			continue
		}
		if p != "" && p != "all" && agent.Protocol != p {
			continue
		}
		if h != "" && agent.Host != h && agent.HostID != h {
			continue
		}
		if _, ok := fleetSkillMatch(agent, skills); !ok {
			continue
		}
		out = append(out, agent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func fleetSkillMatch(agent fleet.AgentRecord, required []string) (int, bool) {
	if len(required) == 0 {
		return len(agent.Skills), true
	}
	have := map[string]bool{}
	for _, skill := range agent.Skills {
		have[skill] = true
	}
	hits := 0
	for _, need := range required {
		if !have[need] {
			return 0, false
		}
		hits++
	}
	return hits, true
}

func matchIDOrName(set map[string]bool, agent fleet.AgentRecord) bool {
	if len(set) == 0 {
		return false
	}
	return set[agent.ID] || set[agent.Name]
}

func printJSON(value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractLeadingTargetArg(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	if strings.HasPrefix(args[0], "-") {
		return "", args
	}
	return args[0], args[1:]
}

func prependTargetArg(target string, rest []string) []string {
	if strings.TrimSpace(target) == "" {
		return rest
	}
	return append([]string{target}, rest...)
}
