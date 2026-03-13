package cli

import (
	"strings"
	"testing"
)

func TestResolveDirectTUITargetsPrefersClusterByName(t *testing.T) {
	targets := []tuiAgentTarget{
		{
			ID:          "cluster:default/support-agent",
			Name:        "support-agent",
			Namespace:   "default",
			DisplayName: "support-agent [cluster default]",
			Source:      tuiTargetSourceCluster,
		},
		{
			ID:          "fleet-support-agent",
			Name:        "support-agent",
			DisplayName: "support-agent [fleet local]",
			Source:      tuiTargetSourceFleet,
		},
	}

	matches := resolveDirectTUITargets(targets, "support-agent")
	if len(matches) != 1 {
		t.Fatalf("expected 1 cluster match, got %d", len(matches))
	}
	if matches[0].Source != tuiTargetSourceCluster {
		t.Fatalf("expected cluster target, got %s", matches[0].Source)
	}
}

func TestResolveDirectTUITargetsFallsBackToFleetID(t *testing.T) {
	targets := []tuiAgentTarget{
		{
			ID:          "inventory-agent-1",
			Name:        "inventory-agent",
			DisplayName: "inventory-agent [fleet local]",
			Source:      tuiTargetSourceFleet,
		},
	}

	matches := resolveDirectTUITargets(targets, "inventory-agent-1")
	if len(matches) != 1 {
		t.Fatalf("expected 1 fleet match, got %d", len(matches))
	}
	if matches[0].Source != tuiTargetSourceFleet {
		t.Fatalf("expected fleet target, got %s", matches[0].Source)
	}
}

func TestRunTUIDirectOpenFleetTarget(t *testing.T) {
	origLoadCombined := loadCombinedTUITargetsFn
	origOpenCluster := openClusterTUITargetFn
	origOpenFleet := openFleetTUITargetFn
	t.Cleanup(func() {
		loadCombinedTUITargetsFn = origLoadCombined
		openClusterTUITargetFn = origOpenCluster
		openFleetTUITargetFn = origOpenFleet
	})

	target := tuiAgentTarget{
		ID:          "agent-123",
		Name:        "agent-123",
		DisplayName: "agent-123 [fleet local]",
		Source:      tuiTargetSourceFleet,
		Endpoint:    "http://127.0.0.1:8080",
	}
	loadCombinedTUITargetsFn = func(namespace, inventoryOverride string) ([]tuiAgentTarget, []string, string, error) {
		return []tuiAgentTarget{target}, nil, "/tmp/inventory.json", nil
	}
	openClusterTUITargetFn = func(target tuiAgentTarget) error {
		t.Fatalf("did not expect cluster target opener")
		return nil
	}

	called := false
	openFleetTUITargetFn = func(got tuiAgentTarget, inventoryPath string) error {
		called = true
		if got.ID != target.ID {
			t.Fatalf("expected target %q, got %q", target.ID, got.ID)
		}
		if inventoryPath != "/tmp/inventory.json" {
			t.Fatalf("expected inventory path to be preserved, got %q", inventoryPath)
		}
		return nil
	}

	if err := runTUI([]string{"agent-123"}); err != nil {
		t.Fatalf("runTUI returned error: %v", err)
	}
	if !called {
		t.Fatal("expected fleet target opener to be called")
	}
}

func TestRunTUILegacyAgentAliasDirectOpenFleetTarget(t *testing.T) {
	origLoadCombined := loadCombinedTUITargetsFn
	origOpenCluster := openClusterTUITargetFn
	origOpenFleet := openFleetTUITargetFn
	t.Cleanup(func() {
		loadCombinedTUITargetsFn = origLoadCombined
		openClusterTUITargetFn = origOpenCluster
		openFleetTUITargetFn = origOpenFleet
	})

	target := tuiAgentTarget{
		ID:          "agent-123",
		Name:        "openclaw-local-http-18789",
		DisplayName: "openclaw-local-http-18789 [fleet local]",
		Source:      tuiTargetSourceFleet,
		Endpoint:    "http://127.0.0.1:18789",
	}
	loadCombinedTUITargetsFn = func(namespace, inventoryOverride string) ([]tuiAgentTarget, []string, string, error) {
		if namespace != "prod" {
			t.Fatalf("expected namespace prod, got %q", namespace)
		}
		return []tuiAgentTarget{target}, nil, "/tmp/inventory.json", nil
	}
	openClusterTUITargetFn = func(target tuiAgentTarget) error {
		t.Fatalf("did not expect cluster target opener")
		return nil
	}

	called := false
	openFleetTUITargetFn = func(got tuiAgentTarget, inventoryPath string) error {
		called = true
		if got.ID != target.ID {
			t.Fatalf("expected target %q, got %q", target.ID, got.ID)
		}
		if inventoryPath != "/tmp/inventory.json" {
			t.Fatalf("expected inventory path to be preserved, got %q", inventoryPath)
		}
		return nil
	}

	if err := runTUI([]string{"agent", "openclaw-local-http-18789", "-n", "prod"}); err != nil {
		t.Fatalf("runTUI returned error: %v", err)
	}
	if !called {
		t.Fatal("expected fleet target opener to be called")
	}
}

func TestRunTUILegacyAgentAliasStillPrefersClusterTarget(t *testing.T) {
	origLoadCombined := loadCombinedTUITargetsFn
	origOpenCluster := openClusterTUITargetFn
	origOpenFleet := openFleetTUITargetFn
	t.Cleanup(func() {
		loadCombinedTUITargetsFn = origLoadCombined
		openClusterTUITargetFn = origOpenCluster
		openFleetTUITargetFn = origOpenFleet
	})

	clusterTarget := tuiAgentTarget{
		ID:          "cluster:prod/support-agent",
		Name:        "support-agent",
		Namespace:   "prod",
		DisplayName: "support-agent [cluster prod]",
		Source:      tuiTargetSourceCluster,
	}
	fleetTarget := tuiAgentTarget{
		ID:          "fleet-support-agent",
		Name:        "support-agent",
		DisplayName: "support-agent [fleet local]",
		Source:      tuiTargetSourceFleet,
	}
	loadCombinedTUITargetsFn = func(namespace, inventoryOverride string) ([]tuiAgentTarget, []string, string, error) {
		return []tuiAgentTarget{clusterTarget, fleetTarget}, nil, "/tmp/inventory.json", nil
	}

	called := false
	openClusterTUITargetFn = func(got tuiAgentTarget) error {
		called = true
		if got.ID != clusterTarget.ID {
			t.Fatalf("expected cluster target %q, got %q", clusterTarget.ID, got.ID)
		}
		return nil
	}
	openFleetTUITargetFn = func(target tuiAgentTarget, inventoryPath string) error {
		t.Fatalf("did not expect fleet target opener")
		return nil
	}

	if err := runTUI([]string{"agent", "support-agent"}); err != nil {
		t.Fatalf("runTUI returned error: %v", err)
	}
	if !called {
		t.Fatal("expected cluster target opener to be called")
	}
}

func TestRunTUINoAgentsReturnsRecoveryHint(t *testing.T) {
	origLoadCombined := loadCombinedTUITargetsFn
	t.Cleanup(func() {
		loadCombinedTUITargetsFn = origLoadCombined
	})

	loadCombinedTUITargetsFn = func(namespace, inventoryOverride string) ([]tuiAgentTarget, []string, string, error) {
		return nil, []string{"cluster agents unavailable: missing kubeconfig"}, "", nil
	}

	err := runTUI(nil)
	if err == nil {
		t.Fatal("expected error when no agents are available")
	}
	msg := err.Error()
	for _, needle := range []string{"agscale cluster doctor", "agscale fleet scan --local --write"} {
		if !strings.Contains(msg, needle) {
			t.Fatalf("expected error %q to contain %q", msg, needle)
		}
	}
}
