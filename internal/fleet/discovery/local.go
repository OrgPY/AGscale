package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"agscale/internal/fleet"
	"agscale/internal/fleet/protocol"
)

type localProcess struct {
	PID       int
	Command   string
	EngineTag string
}

func ScanLocalAgentAware(opts fleet.ProbeOptions, ports []int, signatures []string) []fleet.AgentRecord {
	now := time.Now().UTC()
	procs := listLocalAgentProcesses(signatures)
	if len(procs) == 0 {
		return nil
	}
	pidPorts := localListeningPortsByPID()
	endpointToMeta := map[string]localProcess{}

	for _, p := range procs {
		portsSet := map[int]bool{}
		for _, lp := range pidPorts[p.PID] {
			portsSet[lp] = true
		}
		for _, cp := range extractPortsFromCommand(p.Command) {
			portsSet[cp] = true
		}
		if len(portsSet) == 0 {
			for _, fallback := range ports {
				portsSet[fallback] = true
			}
		}
		for port := range portsSet {
			endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
			endpointToMeta[endpoint] = p
		}
	}

	out := make([]fleet.AgentRecord, 0, len(endpointToMeta))
	for endpoint, proc := range endpointToMeta {
		probe := protocol.ProbeEndpoint(endpoint, opts)
		if !probe.Healthy {
			continue
		}
		model := strings.TrimSpace(probe.Metadata["model"])
		record := fleet.AgentRecord{
			ID:             stableID("local", probe.Protocol, endpoint),
			Name:           inferNameWithEngine("local", probe.Protocol, endpoint, proc.EngineTag),
			Host:           "local",
			HostID:         fleet.StableHostID("local"),
			Source:         "local-scan",
			Protocol:       probe.Protocol,
			Endpoint:       endpoint,
			EndpointSource: fleet.EndpointSourceDiscovered,
			Reachability:   fleet.ReachabilityDirect,
			Health:         fleet.HealthHealthy,
			LatencyMs:      probe.LatencyMs,
			Engine:         proc.EngineTag,
			Runtime:        "process",
			Model:          model,
			Capabilities:   probe.Capabilities,
			LastSeenAt:     now,
			Labels: map[string]string{
				"scope":        "local",
				"discovery":    "process-aware",
				"engine":       proc.EngineTag,
				"processMatch": "true",
			},
			Metadata: map[string]string{
				"pid": strconv.Itoa(proc.PID),
			},
			Resources: fleet.ResourceSnapshot{
				Ports:  portsForEndpoint(endpoint),
				Models: nonEmptyStrings(model),
			},
		}
		for k, v := range probe.Metadata {
			record.Metadata[k] = v
		}
		out = append(out, fleet.RememberDiscoveredAgent(record))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Endpoint < out[j].Endpoint })
	return out
}

func ScanLocal(endpoints []string, opts fleet.ProbeOptions) []fleet.AgentRecord {
	now := time.Now().UTC()
	uniq := map[string]bool{}
	out := make([]fleet.AgentRecord, 0, len(endpoints))
	for _, ep := range endpoints {
		e := normalizeEndpoint(ep)
		if e == "" || uniq[e] {
			continue
		}
		uniq[e] = true
		probe := protocol.ProbeEndpoint(e, opts)
		record := fleet.AgentRecord{
			ID:             stableID("local", probe.Protocol, e),
			Name:           inferName("local", probe.Protocol, e),
			Host:           "local",
			HostID:         fleet.StableHostID("local"),
			Source:         "local-scan",
			Protocol:       probe.Protocol,
			Endpoint:       e,
			EndpointSource: fleet.EndpointSourceDiscovered,
			Reachability:   fleet.ReachabilityDirect,
			Health:         fleet.HealthUnknown,
			LatencyMs:      probe.LatencyMs,
			Engine:         inferEngine(probe),
			Runtime:        "process",
			Model:          strings.TrimSpace(probe.Metadata["model"]),
			Capabilities:   probe.Capabilities,
			LastSeenAt:     now,
			LastError:      probe.LastError,
			Labels:         map[string]string{"scope": "local"},
			Metadata:       probe.Metadata,
			Resources: fleet.ResourceSnapshot{
				Ports:  portsForEndpoint(e),
				Models: nonEmptyStrings(strings.TrimSpace(probe.Metadata["model"])),
			},
		}
		if probe.Healthy {
			record.Health = fleet.HealthHealthy
		} else {
			record.Health = fleet.HealthDegraded
		}
		out = append(out, fleet.RememberDiscoveredAgent(record))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Endpoint < out[j].Endpoint })
	return out
}

func LocalDefaultEndpoints() []string {
	ports := DefaultAgentPorts()
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, fmt.Sprintf("http://127.0.0.1:%d", p))
	}
	return out
}

func DefaultAgentPorts() []int {
	return []int{18789, 8080, 8000, 3000, 5000, 11434}
}

func DefaultAgentSignatures() []string {
	return []string{"openclaw", "opencode", "claude", "aider", "mcp", "autogen", "langchain"}
}

func stableID(host, protocol, endpoint string) string {
	h := sha256.Sum256([]byte(host + "|" + protocol + "|" + endpoint))
	return hex.EncodeToString(h[:10])
}

func inferName(host, protocol, endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "agent-" + host
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	if protocol == "" {
		protocol = "unknown"
	}
	return fmt.Sprintf("%s-%s-%s", host, protocol, port)
}

func inferNameWithEngine(host, protocol, endpoint, engine string) string {
	name := inferName(host, protocol, endpoint)
	if strings.TrimSpace(engine) == "" {
		return name
	}
	return engine + "-" + name
}

func normalizeEndpoint(ep string) string {
	e := strings.TrimSpace(ep)
	if e == "" {
		return ""
	}
	if !strings.HasPrefix(e, "http://") && !strings.HasPrefix(e, "https://") {
		e = "http://" + e
	}
	return strings.TrimSuffix(e, "/")
}

func listLocalAgentProcesses(signatures []string) []localProcess {
	if len(signatures) == 0 {
		signatures = DefaultAgentSignatures()
	}
	cmd := exec.Command("ps", "-axo", "pid=,command=")
	b, err := cmd.Output()
	if err != nil {
		return nil
	}
	sigSet := make([]string, 0, len(signatures))
	for _, s := range signatures {
		t := strings.ToLower(strings.TrimSpace(s))
		if t != "" {
			sigSet = append(sigSet, t)
		}
	}
	out := []localProcess{}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		parts := strings.Fields(t)
		if len(parts) < 2 {
			continue
		}
		pid, err := strconv.Atoi(parts[0])
		if err != nil || pid <= 0 {
			continue
		}
		cmdline := strings.TrimSpace(strings.TrimPrefix(t, parts[0]))
		lower := strings.ToLower(cmdline)
		tag := ""
		for _, sig := range sigSet {
			if strings.Contains(lower, sig) {
				tag = sig
				break
			}
		}
		if tag == "" {
			continue
		}
		out = append(out, localProcess{PID: pid, Command: cmdline, EngineTag: tag})
	}
	return out
}

func localListeningPortsByPID() map[int][]int {
	// lsof output in field format:
	// p<PID>
	// nTCP 127.0.0.1:18789 (LISTEN)
	cmd := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pn")
	b, err := cmd.Output()
	if err != nil {
		return map[int][]int{}
	}
	out := map[int][]int{}
	currentPID := 0
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		switch {
		case strings.HasPrefix(t, "p"):
			pid, err := strconv.Atoi(strings.TrimPrefix(t, "p"))
			if err == nil && pid > 0 {
				currentPID = pid
				if _, ok := out[currentPID]; !ok {
					out[currentPID] = []int{}
				}
			}
		case strings.HasPrefix(t, "n") && currentPID > 0:
			if port := parsePortFromAddr(strings.TrimPrefix(t, "n")); port > 0 {
				out[currentPID] = appendUniquePort(out[currentPID], port)
			}
		}
	}
	return out
}

func extractPortsFromCommand(cmd string) []int {
	tokens := strings.Fields(strings.TrimSpace(cmd))
	out := []int{}
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if strings.HasPrefix(t, "--port=") {
			if p, err := strconv.Atoi(strings.TrimPrefix(t, "--port=")); err == nil {
				out = appendUniquePort(out, p)
			}
			continue
		}
		if t == "--port" || t == "-p" {
			if i+1 < len(tokens) {
				if p, err := strconv.Atoi(strings.Trim(tokens[i+1], " ,")); err == nil {
					out = appendUniquePort(out, p)
				}
			}
			continue
		}
		if strings.Contains(t, ":") {
			if p := parsePortFromAddr(t); p > 0 {
				out = appendUniquePort(out, p)
			}
		}
	}
	return out
}

func parsePortFromAddr(v string) int {
	s := strings.TrimSpace(v)
	if s == "" {
		return 0
	}
	// Trim wrappers like "TCP 127.0.0.1:18789 (LISTEN)"
	if idx := strings.LastIndex(s, " "); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	if idx := strings.LastIndex(s, ":"); idx >= 0 && idx+1 < len(s) {
		p := strings.TrimRight(s[idx+1:], ")")
		port, err := strconv.Atoi(p)
		if err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	return 0
}

func appendUniquePort(in []int, p int) []int {
	if p <= 0 || p > 65535 {
		return in
	}
	for _, e := range in {
		if e == p {
			return in
		}
	}
	return append(in, p)
}

func portsForEndpoint(endpoint string) []int {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 {
		return nil
	}
	return []int{port}
}

func nonEmptyStrings(items ...string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func inferEngine(probe fleet.ProbeResult) string {
	if probe.Metadata != nil {
		if strings.TrimSpace(probe.Metadata["model"]) != "" {
			return "openai-compatible"
		}
		if strings.TrimSpace(probe.Metadata["chatPath"]) == "/chat" {
			return "chat-http"
		}
	}
	if len(probe.Capabilities) > 0 && probe.Capabilities[0] == "mcp-invoke" {
		return "mcp"
	}
	return ""
}
