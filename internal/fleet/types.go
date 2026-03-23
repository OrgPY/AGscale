package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"
)

const (
	HealthHealthy  = "healthy"
	HealthDegraded = "degraded"
	HealthUnknown  = "unknown"

	EndpointSourceDiscovered = "discovered"
	EndpointSourcePinned     = "pinned"

	ReachabilityDirect      = "direct"
	ReachabilitySSHLoopback = "ssh-loopback"

	MetadataChatPath               = "chatPath"
	MetadataModel                  = "model"
	MetadataDefaultModel           = "defaultModel"
	MetadataDiscoveredEndpoint     = "agscale.io/discovered-endpoint"
	MetadataDiscoveredProtocol     = "agscale.io/discovered-protocol"
	MetadataDiscoveredReachability = "agscale.io/discovered-reachability"
	MetadataDiscoveredChatPath     = "agscale.io/discovered-chat-path"
)

type AgentRecord struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Host           string            `json:"host"`
	HostID         string            `json:"hostId,omitempty"`
	Source         string            `json:"source"`
	Protocol       string            `json:"protocol"`
	Endpoint       string            `json:"endpoint"`
	EndpointSource string            `json:"endpointSource,omitempty"`
	Reachability   string            `json:"reachability,omitempty"`
	Health         string            `json:"health"`
	LatencyMs      int64             `json:"latencyMs,omitempty"`
	Engine         string            `json:"engine,omitempty"`
	Runtime        string            `json:"runtime,omitempty"`
	Managed        bool              `json:"managed,omitempty"`
	Version        string            `json:"version,omitempty"`
	Model          string            `json:"model,omitempty"`
	InstallMethod  string            `json:"installMethod,omitempty"`
	Capabilities   []string          `json:"capabilities,omitempty"`
	Skills         []string          `json:"skills,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	LastSeenAt     time.Time         `json:"lastSeenAt,omitempty"`
	LastError      string            `json:"lastError,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Resources      ResourceSnapshot  `json:"resources,omitempty"`
}

type Inventory struct {
	Version   int           `json:"version"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Agents    []AgentRecord `json:"agents"`
}

type ResourceSnapshot struct {
	CPUCores    int      `json:"cpuCores,omitempty"`
	MemoryMiB   int64    `json:"memoryMiB,omitempty"`
	GPUCount    int      `json:"gpuCount,omitempty"`
	DiskFreeMiB int64    `json:"diskFreeMiB,omitempty"`
	Ports       []int    `json:"ports,omitempty"`
	Models      []string `json:"models,omitempty"`
}

type HostRecord struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Address    string            `json:"address"`
	SSHProfile string            `json:"sshProfile,omitempty"`
	Source     string            `json:"source"`
	Health     string            `json:"health"`
	Labels     map[string]string `json:"labels,omitempty"`
	LastSeenAt time.Time         `json:"lastSeenAt,omitempty"`
	LastError  string            `json:"lastError,omitempty"`
	Resources  ResourceSnapshot  `json:"resources,omitempty"`
}

type RunStepResult struct {
	Name       string         `json:"name"`
	AgentID    string         `json:"agentId"`
	Status     string         `json:"status"`
	DurationMs int64          `json:"durationMs,omitempty"`
	Error      string         `json:"error,omitempty"`
	Output     map[string]any `json:"output,omitempty"`
}

type RunRecord struct {
	ID             string          `json:"id"`
	Task           string          `json:"task"`
	Status         string          `json:"status"`
	Topology       string          `json:"topology,omitempty"`
	DryRun         bool            `json:"dryRun,omitempty"`
	Selector       string          `json:"selector,omitempty"`
	RequiredSkills []string        `json:"requiredSkills,omitempty"`
	SelectedAgents []string        `json:"selectedAgents,omitempty"`
	StartedAt      time.Time       `json:"startedAt"`
	CompletedAt    time.Time       `json:"completedAt,omitempty"`
	Error          string          `json:"error,omitempty"`
	Results        []RunStepResult `json:"results,omitempty"`
}

type ProbeOptions struct {
	TimeoutSeconds int
}

type ProbeResult struct {
	Healthy      bool
	Protocol     string
	LatencyMs    int64
	Capabilities []string
	Metadata     map[string]string
	LastError    string
}

func StableHostID(host string) string {
	base := strings.TrimSpace(host)
	if base == "" {
		base = "local"
	}
	if IsLocalHostValue(base) {
		return "host-local"
	}
	sum := sha256.Sum256([]byte(strings.ToLower(base)))
	return "host-" + hex.EncodeToString(sum[:8])
}

func NormalizeAgentRecord(agent AgentRecord) AgentRecord {
	if strings.TrimSpace(agent.EndpointSource) == "" {
		agent.EndpointSource = EndpointSourceDiscovered
	}
	if strings.TrimSpace(agent.Reachability) == "" {
		agent.Reachability = InferAgentReachability(agent)
	}
	if agent.Metadata == nil {
		agent.Metadata = map[string]string{}
	}
	return agent
}

func RememberDiscoveredAgent(agent AgentRecord) AgentRecord {
	agent = NormalizeAgentRecord(agent)
	if agent.Metadata == nil {
		agent.Metadata = map[string]string{}
	}
	agent.Metadata[MetadataDiscoveredEndpoint] = strings.TrimSpace(agent.Endpoint)
	agent.Metadata[MetadataDiscoveredProtocol] = strings.TrimSpace(agent.Protocol)
	agent.Metadata[MetadataDiscoveredReachability] = strings.TrimSpace(agent.Reachability)
	if chatPath := strings.TrimSpace(agent.Metadata[MetadataChatPath]); chatPath != "" {
		agent.Metadata[MetadataDiscoveredChatPath] = chatPath
	}
	return agent
}

func InferAgentReachability(agent AgentRecord) string {
	host := strings.TrimSpace(firstNonEmpty(agent.Host, agent.HostID))
	endpointHost := endpointHostname(agent.Endpoint)
	if !IsLocalHostValue(host) && IsLoopbackHost(endpointHost) {
		return ReachabilitySSHLoopback
	}
	return ReachabilityDirect
}

func IsLocalHostValue(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "local", "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func IsLoopbackHost(host string) bool {
	switch strings.TrimSpace(strings.ToLower(host)) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func endpointHostname(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	u, err := url.Parse(v)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	if idx := strings.Index(v, "://"); idx >= 0 {
		v = v[idx+3:]
	}
	if slash := strings.Index(v, "/"); slash >= 0 {
		v = v[:slash]
	}
	if colon := strings.Index(v, ":"); colon >= 0 {
		v = v[:colon]
	}
	return strings.TrimSpace(v)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
