package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestNormalizeAgentRecordInitializesMetadataAndDefaults(t *testing.T) {
	t.Parallel()

	agent := NormalizeAgentRecord(AgentRecord{
		Host:     "devbox-1",
		Endpoint: "http://127.0.0.1:8080",
	})

	if agent.Metadata == nil {
		t.Fatal("expected metadata to be initialized")
	}
	if agent.EndpointSource != EndpointSourceDiscovered {
		t.Fatalf("expected endpoint source %q, got %q", EndpointSourceDiscovered, agent.EndpointSource)
	}
	if agent.Reachability != ReachabilitySSHLoopback {
		t.Fatalf("expected reachability %q, got %q", ReachabilitySSHLoopback, agent.Reachability)
	}
}

func TestStableHostIDUsesSHA256ForRemoteHosts(t *testing.T) {
	t.Parallel()

	if got := StableHostID("localhost"); got != "host-local" {
		t.Fatalf("expected local host id, got %q", got)
	}

	sum := sha256.Sum256([]byte("devbox-1"))
	want := "host-" + hex.EncodeToString(sum[:8])
	if got := StableHostID("DevBox-1"); got != want {
		t.Fatalf("expected remote host id %q, got %q", want, got)
	}
}

func TestInferAgentReachability(t *testing.T) {
	t.Parallel()

	sshLoopback := InferAgentReachability(AgentRecord{
		Host:     "remote-1",
		Endpoint: "http://127.0.0.1:11434",
	})
	if sshLoopback != ReachabilitySSHLoopback {
		t.Fatalf("expected ssh-loopback reachability, got %q", sshLoopback)
	}

	direct := InferAgentReachability(AgentRecord{
		Host:     "remote-1",
		Endpoint: "https://agent.example",
	})
	if direct != ReachabilityDirect {
		t.Fatalf("expected direct reachability, got %q", direct)
	}
}

func TestHostValueHelpers(t *testing.T) {
	t.Parallel()

	localValues := []string{"", "local", "LOCALHOST", "127.0.0.1", "::1"}
	for _, value := range localValues {
		if !IsLocalHostValue(value) {
			t.Fatalf("expected %q to be treated as local host value", value)
		}
	}

	loopbackValues := []string{"localhost", "127.0.0.1", "::1"}
	for _, value := range loopbackValues {
		if !IsLoopbackHost(value) {
			t.Fatalf("expected %q to be treated as loopback host", value)
		}
	}

	if IsLoopbackHost("agent.example") {
		t.Fatal("did not expect non-loopback host to be treated as loopback")
	}
}
