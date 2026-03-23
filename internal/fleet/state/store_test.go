package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agscale/internal/fleet"
)

func TestAppendRunSortsNewestFirstAndAppliesLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "runs.json")
	runs := []fleet.RunRecord{
		{ID: "older", Task: "older", StartedAt: time.Unix(10, 0).UTC()},
		{ID: "newer", Task: "newer", StartedAt: time.Unix(20, 0).UTC()},
	}
	if err := SaveRuns(path, runs); err != nil {
		t.Fatalf("SaveRuns returned error: %v", err)
	}

	if err := AppendRun(path, fleet.RunRecord{ID: "latest", Task: "latest", StartedAt: time.Unix(30, 0).UTC()}, 2); err != nil {
		t.Fatalf("AppendRun returned error: %v", err)
	}

	got, err := LoadRuns(path)
	if err != nil {
		t.Fatalf("LoadRuns returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected limit to keep 2 runs, got %d", len(got))
	}
	if got[0].ID != "latest" || got[1].ID != "newer" {
		t.Fatalf("expected newest runs first, got %+v", got)
	}
}

func TestMergeHostsPreservesManualLabels(t *testing.T) {
	t.Parallel()

	existing := []fleet.HostRecord{{
		ID:      "host-1",
		Name:    "devbox-1",
		Address: "devbox-1",
		Labels:  map[string]string{"env": "prod"},
		Health:  fleet.HealthUnknown,
	}}
	discovered := []fleet.HostRecord{{
		ID:      "host-1",
		Name:    "devbox-1",
		Address: "devbox-1",
		Labels:  map[string]string{"scope": "ssh"},
		Health:  fleet.HealthHealthy,
	}}

	merged := MergeHosts(existing, discovered)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged host, got %d", len(merged))
	}
	if merged[0].Labels["env"] != "prod" || merged[0].Labels["scope"] != "ssh" {
		t.Fatalf("expected manual and discovered labels to be merged, got %+v", merged[0].Labels)
	}
	if merged[0].Health != fleet.HealthHealthy {
		t.Fatalf("expected discovered health to win, got %q", merged[0].Health)
	}
}

func TestMergeLabelsReturnsNonNilMap(t *testing.T) {
	t.Parallel()

	got := mergeLabels(nil, nil)
	if got == nil {
		t.Fatal("expected non-nil labels map")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty labels map, got %+v", got)
	}
}

func TestSaveLoadHostsFindAndRemove(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "hosts.json")
	hosts := []fleet.HostRecord{
		{ID: "host-b", Name: "devbox-b", Address: "10.0.0.2", Health: fleet.HealthHealthy},
		{ID: "host-a", Name: "devbox-a", Address: "10.0.0.1", Health: fleet.HealthHealthy},
	}
	if err := SaveHosts(path, hosts); err != nil {
		t.Fatalf("SaveHosts returned error: %v", err)
	}

	loaded, err := LoadHosts(path)
	if err != nil {
		t.Fatalf("LoadHosts returned error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(loaded))
	}
	if loaded[0].Name != "devbox-a" {
		t.Fatalf("expected hosts to be sorted by name, got %+v", loaded)
	}

	if host, _, ok := FindHost(loaded, "host-b"); !ok || host.Name != "devbox-b" {
		t.Fatalf("expected FindHost by id to match host-b, got %+v ok=%t", host, ok)
	}
	if host, _, ok := FindHost(loaded, "devbox-a"); !ok || host.ID != "host-a" {
		t.Fatalf("expected FindHost by name to match host-a, got %+v ok=%t", host, ok)
	}
	if host, _, ok := FindHost(loaded, "10.0.0.2"); !ok || host.ID != "host-b" {
		t.Fatalf("expected FindHost by address to match host-b, got %+v ok=%t", host, ok)
	}

	remaining, removed, ok := RemoveHost(loaded, "host-a")
	if !ok || removed.ID != "host-a" || len(remaining) != 1 {
		t.Fatalf("unexpected RemoveHost result remaining=%+v removed=%+v ok=%t", remaining, removed, ok)
	}
	if _, _, ok := RemoveHost(remaining, "missing"); ok {
		t.Fatal("did not expect RemoveHost to succeed for a missing host")
	}
}

func TestWriteJSONFileDoesNotCorruptExistingFileOnMarshalError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "hosts.json")
	if err := writeJSONFile(path, hostFile{
		Version: 1,
		Hosts: []fleet.HostRecord{{
			ID:   "host-1",
			Name: "devbox-1",
		}},
	}); err != nil {
		t.Fatalf("writeJSONFile initial write returned error: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read initial file: %v", err)
	}

	err = writeJSONFile(path, failingJSON{})
	if err == nil {
		t.Fatal("expected marshal failure to be returned")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read file after failed write: %v", readErr)
	}
	if string(before) != string(after) {
		t.Fatalf("expected original file contents to be preserved, before=%q after=%q", string(before), string(after))
	}
}

type failingJSON struct{}

func (failingJSON) MarshalJSON() ([]byte, error) {
	return nil, errors.New("boom")
}
