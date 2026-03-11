package fleet

import "time"

const (
	HealthHealthy  = "healthy"
	HealthDegraded = "degraded"
	HealthUnknown  = "unknown"
)

type AgentRecord struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Source       string            `json:"source"`
	Protocol     string            `json:"protocol"`
	Endpoint     string            `json:"endpoint"`
	Health       string            `json:"health"`
	LatencyMs    int64             `json:"latencyMs,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Skills       []string          `json:"skills,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	LastSeenAt   time.Time         `json:"lastSeenAt,omitempty"`
	LastError    string            `json:"lastError,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Inventory struct {
	Version   int           `json:"version"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Agents    []AgentRecord `json:"agents"`
}

type ProbeOptions struct {
	TimeoutSeconds int
}

type ProbeResult struct {
	Healthy      bool
	Protocol     string
	LatencyMs    int64
	Capabilities []string
	LastError    string
}
