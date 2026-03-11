package inventory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"agscale/internal/fleet"
)

const defaultPathSuffix = ".agscale/inventory/agents.json"

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultPathSuffix), nil
}

func Load(path string) (fleet.Inventory, error) {
	out := fleet.Inventory{Version: 1, Agents: []fleet.AgentRecord{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return out, err
	}
	if len(b) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	if out.Version == 0 {
		out.Version = 1
	}
	if out.Agents == nil {
		out.Agents = []fleet.AgentRecord{}
	}
	return out, nil
}

func Save(path string, inv fleet.Inventory) error {
	if inv.Version == 0 {
		inv.Version = 1
	}
	inv.UpdatedAt = time.Now().UTC()
	sort.Slice(inv.Agents, func(i, j int) bool { return inv.Agents[i].ID < inv.Agents[j].ID })

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	return nil
}

func Merge(existing fleet.Inventory, discovered []fleet.AgentRecord) fleet.Inventory {
	now := time.Now().UTC()
	merged := fleet.Inventory{
		Version:   1,
		UpdatedAt: now,
		Agents:    make([]fleet.AgentRecord, 0, len(existing.Agents)+len(discovered)),
	}

	seen := map[string]bool{}
	byID := map[string]fleet.AgentRecord{}
	for _, a := range existing.Agents {
		byID[a.ID] = a
	}
	for _, a := range discovered {
		a.LastSeenAt = now
		byID[a.ID] = a
		seen[a.ID] = true
	}
	for id, a := range byID {
		if !seen[id] {
			a.Health = fleet.HealthDegraded
			if a.LastError == "" {
				a.LastError = "not seen in latest scan"
			}
		}
		merged.Agents = append(merged.Agents, a)
	}
	sort.Slice(merged.Agents, func(i, j int) bool { return merged.Agents[i].ID < merged.Agents[j].ID })
	return merged
}
