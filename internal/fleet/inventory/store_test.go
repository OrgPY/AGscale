package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"agscale/internal/fleet"
)

func TestLoadFallsBackToLegacyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	legacyPath, err := LegacyPath()
	if err != nil {
		t.Fatalf("LegacyPath returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	payload := []byte(`{"version":1,"agents":[{"id":"agent-1","name":"agent-1","host":"devbox-1","protocol":"http","endpoint":"http://127.0.0.1:8080","health":"healthy"}]}`)
	if err := os.WriteFile(legacyPath, payload, 0o600); err != nil {
		t.Fatalf("write legacy inventory: %v", err)
	}

	defaultPath, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath returned error: %v", err)
	}
	inv, err := Load(defaultPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(inv.Agents) != 1 {
		t.Fatalf("expected 1 agent from legacy inventory, got %d", len(inv.Agents))
	}
	if inv.Agents[0].ID != "agent-1" {
		t.Fatalf("expected legacy agent to be loaded, got %+v", inv.Agents[0])
	}
	if inv.Agents[0].Reachability != fleet.ReachabilitySSHLoopback {
		t.Fatalf("expected legacy remote loopback endpoint to normalize to ssh-loopback, got %+v", inv.Agents[0])
	}
}

func TestMergePreservesNewFields(t *testing.T) {
	t.Parallel()

	existing := fleet.Inventory{
		Version: 1,
		Agents: []fleet.AgentRecord{{
			ID:      "agent-1",
			Name:    "agent-1",
			HostID:  "host-local",
			Engine:  "openclaw",
			Model:   "demo-model",
			Managed: true,
		}},
	}
	discovered := []fleet.AgentRecord{{
		ID:       "agent-1",
		Name:     "agent-1",
		HostID:   "host-local",
		Engine:   "openclaw",
		Model:    "demo-model-v2",
		Managed:  false,
		Protocol: "http",
	}}

	merged := Merge(existing, discovered)
	if len(merged.Agents) != 1 {
		t.Fatalf("expected 1 merged agent, got %d", len(merged.Agents))
	}
	if merged.Agents[0].Model != "demo-model-v2" {
		t.Fatalf("expected discovered model to win, got %q", merged.Agents[0].Model)
	}
	if merged.Agents[0].Protocol != "http" {
		t.Fatalf("expected discovered protocol to be preserved, got %q", merged.Agents[0].Protocol)
	}
}

func TestMergePreservesPinnedEndpointSettings(t *testing.T) {
	t.Parallel()

	existing := fleet.Inventory{
		Version: 1,
		Agents: []fleet.AgentRecord{{
			ID:             "agent-1",
			Name:           "agent-1",
			Host:           "devbox-1",
			Endpoint:       "https://agent.example",
			EndpointSource: fleet.EndpointSourcePinned,
			Reachability:   fleet.ReachabilityDirect,
			Protocol:       "http",
			Metadata: map[string]string{
				fleet.MetadataChatPath:               "/custom",
				fleet.MetadataDiscoveredEndpoint:     "http://127.0.0.1:18789",
				fleet.MetadataDiscoveredProtocol:     "http",
				fleet.MetadataDiscoveredReachability: fleet.ReachabilitySSHLoopback,
			},
		}},
	}
	discovered := []fleet.AgentRecord{{
		ID:             "agent-1",
		Name:           "agent-1",
		Host:           "devbox-1",
		Endpoint:       "http://127.0.0.1:18789",
		EndpointSource: fleet.EndpointSourceDiscovered,
		Reachability:   fleet.ReachabilitySSHLoopback,
		Protocol:       "http",
		Metadata: map[string]string{
			fleet.MetadataChatPath: "/chat",
		},
	}}

	merged := Merge(existing, discovered)
	if len(merged.Agents) != 1 {
		t.Fatalf("expected 1 merged agent, got %d", len(merged.Agents))
	}
	agent := merged.Agents[0]
	if agent.Endpoint != "https://agent.example" {
		t.Fatalf("expected pinned endpoint to be preserved, got %q", agent.Endpoint)
	}
	if agent.EndpointSource != fleet.EndpointSourcePinned {
		t.Fatalf("expected endpoint source pinned, got %q", agent.EndpointSource)
	}
	if agent.Reachability != fleet.ReachabilityDirect {
		t.Fatalf("expected pinned reachability to be preserved, got %q", agent.Reachability)
	}
	if agent.Metadata[fleet.MetadataChatPath] != "/custom" {
		t.Fatalf("expected pinned chat path to be preserved, got %+v", agent.Metadata)
	}
	if agent.Metadata[fleet.MetadataDiscoveredEndpoint] != "http://127.0.0.1:18789" {
		t.Fatalf("expected discovered endpoint metadata to be retained, got %+v", agent.Metadata)
	}
}

func TestMergeAgentMetadataReturnsNonNilMap(t *testing.T) {
	t.Parallel()

	got := mergeAgentMetadata(nil, nil)
	if got == nil {
		t.Fatal("expected non-nil metadata map")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty metadata map, got %+v", got)
	}

	got = mergeAgentMetadata(map[string]string{"chatPath": "/chat"}, map[string]string{"chatPath": "/custom", "model": "demo"})
	if got["chatPath"] != "/custom" || got["model"] != "demo" {
		t.Fatalf("expected discovered metadata to win conflicts, got %+v", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "agents.json")
	inv := fleet.Inventory{
		Version: 1,
		Agents: []fleet.AgentRecord{{
			ID:       "agent-1",
			Name:     "agent-1",
			Host:     "local",
			Protocol: "http",
			Endpoint: "http://127.0.0.1:8080",
			Health:   fleet.HealthHealthy,
		}},
	}
	if err := Save(path, inv); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Agents) != 1 {
		t.Fatalf("expected 1 loaded agent, got %d", len(loaded.Agents))
	}
	if loaded.Agents[0].ID != "agent-1" || loaded.Agents[0].Metadata == nil {
		t.Fatalf("unexpected loaded agent %+v", loaded.Agents[0])
	}
}

func TestLoadCorruptedJSONReturnsError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("write corrupted payload: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected corrupted inventory to return an error")
	}
}

func TestMergeMarksUnseenAgentsDegraded(t *testing.T) {
	t.Parallel()

	existing := fleet.Inventory{
		Version: 1,
		Agents: []fleet.AgentRecord{
			{ID: "agent-1", Name: "agent-1", Endpoint: "http://127.0.0.1:8080", Health: fleet.HealthHealthy},
			{ID: "agent-2", Name: "agent-2", Endpoint: "http://127.0.0.1:8081", Health: fleet.HealthHealthy},
		},
	}

	merged := Merge(existing, []fleet.AgentRecord{
		{ID: "agent-1", Name: "agent-1", Endpoint: "http://127.0.0.1:8080", Protocol: "http", Health: fleet.HealthHealthy},
	})

	if len(merged.Agents) != 2 {
		t.Fatalf("expected 2 merged agents, got %d", len(merged.Agents))
	}
	for _, agent := range merged.Agents {
		if agent.ID != "agent-2" {
			continue
		}
		if agent.Health != fleet.HealthDegraded {
			t.Fatalf("expected unseen agent to be degraded, got %+v", agent)
		}
		if agent.LastError == "" {
			t.Fatalf("expected unseen agent to record last error, got %+v", agent)
		}
		return
	}
	t.Fatal("expected merged inventory to keep agent-2")
}
