package discovery

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"agscale/internal/fleet"
	"agscale/internal/fleet/protocol"
)

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
			ID:           stableID("local", probe.Protocol, e),
			Name:         inferName("local", probe.Protocol, e),
			Host:         "local",
			Source:       "local-scan",
			Protocol:     probe.Protocol,
			Endpoint:     e,
			Health:       fleet.HealthUnknown,
			LatencyMs:    probe.LatencyMs,
			Capabilities: probe.Capabilities,
			LastSeenAt:   now,
			LastError:    probe.LastError,
			Labels:       map[string]string{"scope": "local"},
		}
		if probe.Healthy {
			record.Health = fleet.HealthHealthy
		} else {
			record.Health = fleet.HealthDegraded
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Endpoint < out[j].Endpoint })
	return out
}

func LocalDefaultEndpoints() []string {
	ports := []int{18789, 8080, 8000, 3000, 5000, 11434}
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, fmt.Sprintf("http://127.0.0.1:%d", p))
	}
	return out
}

func stableID(host, protocol, endpoint string) string {
	h := sha1.Sum([]byte(host + "|" + protocol + "|" + endpoint))
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
