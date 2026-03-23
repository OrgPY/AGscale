package cli

import (
	"strings"
	"testing"

	"agscale/internal/fleet"
)

func TestAgentNeedsSSHTunnel(t *testing.T) {
	t.Parallel()

	if agentNeedsSSHTunnel(fleet.AgentRecord{
		Host:         "local",
		Endpoint:     "http://127.0.0.1:8080",
		Reachability: fleet.ReachabilitySSHLoopback,
	}) {
		t.Fatal("did not expect local agent to require ssh tunnel")
	}

	if !agentNeedsSSHTunnel(fleet.AgentRecord{
		Host:         "devbox-1",
		Endpoint:     "http://127.0.0.1:8080",
		Reachability: fleet.ReachabilitySSHLoopback,
	}) {
		t.Fatal("expected remote ssh-loopback agent to require ssh tunnel")
	}
}

func TestTunnelCacheKeyIsDeterministic(t *testing.T) {
	t.Parallel()

	keyA := tunnelCacheKey("devbox-1", "http://127.0.0.1:18789")
	keyB := tunnelCacheKey("devbox-1", "http://127.0.0.1:18789")
	if keyA != keyB {
		t.Fatalf("expected deterministic cache key, got %q and %q", keyA, keyB)
	}
}

func TestSanitizeTunnelError(t *testing.T) {
	t.Parallel()

	raw := strings.Repeat("x", 200) + "\nmore"
	got := sanitizeTunnelError(raw)
	if strings.Contains(got, "\n") {
		t.Fatalf("expected newlines to be stripped, got %q", got)
	}
	if len(got) > 160 {
		t.Fatalf("expected sanitized message to be truncated, got length %d", len(got))
	}
}

func TestDefaultFleetSSHConfig(t *testing.T) {
	t.Setenv("AGSCALE_FLEET_SSH_CONFIG", "/tmp/custom-ssh-config")
	if got := defaultFleetSSHConfig(); got != "/tmp/custom-ssh-config" {
		t.Fatalf("expected env override, got %q", got)
	}

	t.Setenv("AGSCALE_FLEET_SSH_CONFIG", "")
	if got := defaultFleetSSHConfig(); got != "~/.ssh/config" {
		t.Fatalf("expected default ssh config path, got %q", got)
	}
}
