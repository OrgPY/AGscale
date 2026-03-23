package cli

import (
	"path/filepath"
	"testing"
	"time"

	"agscale/internal/fleet"
	"agscale/internal/fleet/inventory"
)

func TestLoadFleetHostsDerivesHostsFromAgentInventory(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	invPath := filepath.Join(stateDir, "agents.json")
	inv := fleet.Inventory{
		Version: 1,
		Agents: []fleet.AgentRecord{{
			ID:         "agent-1",
			Name:       "agent-1",
			Host:       "devbox-1",
			HostID:     "host-devbox-1",
			Protocol:   "http",
			Endpoint:   "http://127.0.0.1:8080",
			Health:     fleet.HealthHealthy,
			LastSeenAt: time.Unix(100, 0).UTC(),
			Model:      "gpt-test",
			Resources: fleet.ResourceSnapshot{
				Ports:  []int{8080},
				Models: []string{"gpt-test"},
			},
		}},
	}
	if err := inventory.Save(invPath, inv); err != nil {
		t.Fatalf("inventory.Save returned error: %v", err)
	}

	hosts, agents, err := loadFleetHosts(stateDir, invPath)
	if err != nil {
		t.Fatalf("loadFleetHosts returned error: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 derived host, got %d", len(hosts))
	}
	if hosts[0].ID != "host-devbox-1" {
		t.Fatalf("expected derived host to keep host id, got %q", hosts[0].ID)
	}
	if len(hosts[0].Resources.Ports) != 1 || hosts[0].Resources.Ports[0] != 8080 {
		t.Fatalf("expected derived host ports to include agent port, got %+v", hosts[0].Resources.Ports)
	}
	if len(hosts[0].Resources.Models) != 1 || hosts[0].Resources.Models[0] != "gpt-test" {
		t.Fatalf("expected derived host models to include agent model, got %+v", hosts[0].Resources.Models)
	}
}

func TestResolveFleetPathsDefaultsToFleetStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stateDir, invPath, hostsPath, runsPath, err := resolveFleetPaths("", "")
	if err != nil {
		t.Fatalf("resolveFleetPaths returned error: %v", err)
	}
	wantDir := filepath.Join(home, ".agscale", "fleet")
	if stateDir != wantDir {
		t.Fatalf("expected state dir %q, got %q", wantDir, stateDir)
	}
	if invPath != filepath.Join(wantDir, "agents.json") {
		t.Fatalf("unexpected inventory path %q", invPath)
	}
	if hostsPath != filepath.Join(wantDir, "hosts.json") {
		t.Fatalf("unexpected hosts path %q", hostsPath)
	}
	if runsPath != filepath.Join(wantDir, "runs.json") {
		t.Fatalf("unexpected runs path %q", runsPath)
	}
}

func TestRunFleetAgentPinAndUnpin(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	invPath := filepath.Join(stateDir, "agents.json")
	inv := fleet.Inventory{
		Version: 1,
		Agents: []fleet.AgentRecord{{
			ID:             "agent-1",
			Name:           "agent-1",
			Host:           "devbox-1",
			HostID:         "host-devbox-1",
			Protocol:       "http",
			Endpoint:       "http://127.0.0.1:18789",
			EndpointSource: fleet.EndpointSourceDiscovered,
			Reachability:   fleet.ReachabilitySSHLoopback,
			Health:         fleet.HealthHealthy,
			Metadata: map[string]string{
				fleet.MetadataChatPath: "/chat",
			},
		}},
	}
	if err := inventory.Save(invPath, inv); err != nil {
		t.Fatalf("inventory.Save returned error: %v", err)
	}

	if err := runFleetAgent([]string{
		"pin", "agent-1",
		"--endpoint", "https://agent.example",
		"--reachability", "direct",
		"--chat-path", "/custom",
		"--inventory", invPath,
		"--state-dir", stateDir,
	}); err != nil {
		t.Fatalf("runFleetAgent pin returned error: %v", err)
	}

	agents, err := loadFleetAgents(invPath)
	if err != nil {
		t.Fatalf("loadFleetAgents returned error after pin: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 pinned agent, got %d", len(agents))
	}
	if agents[0].EndpointSource != fleet.EndpointSourcePinned || agents[0].Endpoint != "https://agent.example" {
		t.Fatalf("expected pinned endpoint to be stored, got %+v", agents[0])
	}
	if agents[0].Metadata[fleet.MetadataDiscoveredEndpoint] != "http://127.0.0.1:18789" {
		t.Fatalf("expected discovered endpoint fallback to be stored, got %+v", agents[0].Metadata)
	}

	if err := runFleetAgent([]string{
		"unpin", "agent-1",
		"--inventory", invPath,
		"--state-dir", stateDir,
	}); err != nil {
		t.Fatalf("runFleetAgent unpin returned error: %v", err)
	}

	agents, err = loadFleetAgents(invPath)
	if err != nil {
		t.Fatalf("loadFleetAgents returned error after unpin: %v", err)
	}
	if agents[0].EndpointSource != fleet.EndpointSourceDiscovered {
		t.Fatalf("expected agent to return to discovered source, got %+v", agents[0])
	}
	if agents[0].Endpoint != "http://127.0.0.1:18789" || agents[0].Reachability != fleet.ReachabilitySSHLoopback {
		t.Fatalf("expected discovered endpoint to be restored, got %+v", agents[0])
	}
	if agents[0].Metadata[fleet.MetadataChatPath] != "/chat" {
		t.Fatalf("expected discovered chat path to be restored, got %+v", agents[0].Metadata)
	}
}

func TestFleetTunnelManagerPreparesSSHTunneledAgents(t *testing.T) {
	t.Parallel()

	orig := startFleetSSHTunnelFn
	t.Cleanup(func() {
		startFleetSSHTunnelFn = orig
	})

	calls := 0
	startFleetSSHTunnelFn = func(agent fleet.AgentRecord, sshConfigPath string) (string, func(), error) {
		calls++
		if sshConfigPath != "/tmp/ssh-config" {
			t.Fatalf("expected ssh config to be passed through, got %q", sshConfigPath)
		}
		return "http://127.0.0.1:45678", func() {}, nil
	}

	manager := newFleetTunnelManager("/tmp/ssh-config")
	defer manager.Close()

	prepared, err := manager.PrepareAgent(fleet.AgentRecord{
		ID:           "agent-1",
		Name:         "agent-1",
		Host:         "devbox-1",
		Endpoint:     "http://127.0.0.1:18789",
		Reachability: fleet.ReachabilitySSHLoopback,
	})
	if err != nil {
		t.Fatalf("PrepareAgent returned error: %v", err)
	}
	if prepared.Endpoint != "http://127.0.0.1:45678" {
		t.Fatalf("expected tunneled endpoint, got %q", prepared.Endpoint)
	}
	if calls != 1 {
		t.Fatalf("expected one tunnel setup, got %d", calls)
	}

	_, err = manager.PrepareAgent(fleet.AgentRecord{
		ID:           "agent-1",
		Name:         "agent-1",
		Host:         "devbox-1",
		Endpoint:     "http://127.0.0.1:18789",
		Reachability: fleet.ReachabilitySSHLoopback,
	})
	if err != nil {
		t.Fatalf("second PrepareAgent returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected tunnel session reuse, got %d starts", calls)
	}
}
