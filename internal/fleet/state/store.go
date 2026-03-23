package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"agscale/internal/fleet"
)

type hostFile struct {
	Version   int                `json:"version"`
	UpdatedAt time.Time          `json:"updatedAt"`
	Hosts     []fleet.HostRecord `json:"hosts"`
}

type runFile struct {
	Version   int               `json:"version"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Runs      []fleet.RunRecord `json:"runs"`
}

func LoadHosts(path string) ([]fleet.HostRecord, error) {
	out := hostFile{Version: 1, Hosts: []fleet.HostRecord{}}
	if err := readJSONFile(path, &out); err != nil {
		return nil, err
	}
	if out.Hosts == nil {
		out.Hosts = []fleet.HostRecord{}
	}
	sort.Slice(out.Hosts, func(i, j int) bool {
		if out.Hosts[i].Name != out.Hosts[j].Name {
			return out.Hosts[i].Name < out.Hosts[j].Name
		}
		return out.Hosts[i].ID < out.Hosts[j].ID
	})
	return out.Hosts, nil
}

func SaveHosts(path string, hosts []fleet.HostRecord) error {
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Name != hosts[j].Name {
			return hosts[i].Name < hosts[j].Name
		}
		return hosts[i].ID < hosts[j].ID
	})
	return writeJSONFile(path, hostFile{
		Version:   1,
		UpdatedAt: time.Now().UTC(),
		Hosts:     hosts,
	})
}

func MergeHosts(existing []fleet.HostRecord, discovered []fleet.HostRecord) []fleet.HostRecord {
	byID := map[string]fleet.HostRecord{}
	for _, host := range existing {
		byID[host.ID] = host
	}
	for _, host := range discovered {
		current, ok := byID[host.ID]
		if ok {
			host.Labels = mergeLabels(current.Labels, host.Labels)
			if host.Name == "" {
				host.Name = current.Name
			}
			if host.Address == "" {
				host.Address = current.Address
			}
			if host.SSHProfile == "" {
				host.SSHProfile = current.SSHProfile
			}
			if host.Source == "" {
				host.Source = current.Source
			}
			if host.LastSeenAt.IsZero() {
				host.LastSeenAt = current.LastSeenAt
			}
			if host.LastError == "" {
				host.LastError = current.LastError
			}
			host.Resources = mergeResources(current.Resources, host.Resources)
		}
		byID[host.ID] = host
	}

	out := make([]fleet.HostRecord, 0, len(byID))
	for _, host := range byID {
		out = append(out, host)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func FindHost(hosts []fleet.HostRecord, target string) (fleet.HostRecord, int, bool) {
	for i, host := range hosts {
		if host.ID == target || host.Name == target || host.Address == target {
			return host, i, true
		}
	}
	return fleet.HostRecord{}, -1, false
}

func RemoveHost(hosts []fleet.HostRecord, target string) ([]fleet.HostRecord, fleet.HostRecord, bool) {
	host, idx, ok := FindHost(hosts, target)
	if !ok {
		return hosts, fleet.HostRecord{}, false
	}
	out := append([]fleet.HostRecord{}, hosts[:idx]...)
	out = append(out, hosts[idx+1:]...)
	return out, host, true
}

func LoadRuns(path string) ([]fleet.RunRecord, error) {
	out := runFile{Version: 1, Runs: []fleet.RunRecord{}}
	if err := readJSONFile(path, &out); err != nil {
		return nil, err
	}
	if out.Runs == nil {
		out.Runs = []fleet.RunRecord{}
	}
	sortRuns(out.Runs)
	return out.Runs, nil
}

func SaveRuns(path string, runs []fleet.RunRecord) error {
	sortRuns(runs)
	return writeJSONFile(path, runFile{
		Version:   1,
		UpdatedAt: time.Now().UTC(),
		Runs:      runs,
	})
}

func AppendRun(path string, run fleet.RunRecord, limit int) error {
	runs, err := LoadRuns(path)
	if err != nil {
		return err
	}
	runs = append(runs, run)
	sortRuns(runs)
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return SaveRuns(path, runs)
}

func FindRun(runs []fleet.RunRecord, target string) (fleet.RunRecord, bool) {
	for _, run := range runs {
		if run.ID == target {
			return run, true
		}
	}
	return fleet.RunRecord{}, false
}

func readJSONFile(path string, out any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}

func writeJSONFile(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()

	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := os.WriteFile(tmpPath, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func sortRuns(runs []fleet.RunRecord) {
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].StartedAt.After(runs[j].StartedAt)
		}
		return runs[i].ID > runs[j].ID
	})
}

func mergeLabels(existing map[string]string, discovered map[string]string) map[string]string {
	if len(existing) == 0 && len(discovered) == 0 {
		return map[string]string{}
	}
	out := map[string]string{}
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range discovered {
		out[k] = v
	}
	return out
}

func mergeResources(existing fleet.ResourceSnapshot, discovered fleet.ResourceSnapshot) fleet.ResourceSnapshot {
	if discovered.CPUCores == 0 {
		discovered.CPUCores = existing.CPUCores
	}
	if discovered.MemoryMiB == 0 {
		discovered.MemoryMiB = existing.MemoryMiB
	}
	if discovered.GPUCount == 0 {
		discovered.GPUCount = existing.GPUCount
	}
	if discovered.DiskFreeMiB == 0 {
		discovered.DiskFreeMiB = existing.DiskFreeMiB
	}
	if len(discovered.Ports) == 0 {
		discovered.Ports = existing.Ports
	}
	if len(discovered.Models) == 0 {
		discovered.Models = existing.Models
	}
	return discovered
}
