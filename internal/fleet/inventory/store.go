package inventory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agscale/internal/fleet"
)

const (
	defaultPathSuffix = ".agscale/fleet/agents.json"
	legacyPathSuffix  = ".agscale/inventory/agents.json"
)

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultPathSuffix), nil
}

func LegacyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, legacyPathSuffix), nil
}

func Load(path string) (fleet.Inventory, error) {
	out := fleet.Inventory{Version: 1, Agents: []fleet.AgentRecord{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			legacyPath, legacyErr := LegacyPath()
			defaultPath, defaultErr := DefaultPath()
			if legacyErr == nil && defaultErr == nil && path == defaultPath {
				legacyBytes, legacyReadErr := os.ReadFile(legacyPath)
				if legacyReadErr == nil {
					b = legacyBytes
				} else if errors.Is(legacyReadErr, os.ErrNotExist) {
					return out, nil
				} else {
					return out, legacyReadErr
				}
			} else {
				return out, nil
			}
		} else {
			return out, err
		}
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
	for i := range out.Agents {
		out.Agents[i] = fleet.NormalizeAgentRecord(out.Agents[i])
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
	return writeFileAtomically(path, b)
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
		byID[a.ID] = fleet.NormalizeAgentRecord(a)
	}
	for _, a := range discovered {
		a = fleet.RememberDiscoveredAgent(a)
		a.LastSeenAt = now
		if current, ok := byID[a.ID]; ok {
			a = mergeAgentRecord(current, a)
		}
		byID[a.ID] = fleet.NormalizeAgentRecord(a)
		seen[a.ID] = true
	}
	for id, a := range byID {
		a = fleet.NormalizeAgentRecord(a)
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

func mergeAgentRecord(existing, discovered fleet.AgentRecord) fleet.AgentRecord {
	existing = fleet.NormalizeAgentRecord(existing)
	discovered = fleet.RememberDiscoveredAgent(discovered)
	discovered.Metadata = mergeAgentMetadata(existing.Metadata, discovered.Metadata)

	if existing.EndpointSource == fleet.EndpointSourcePinned {
		discovered.EndpointSource = fleet.EndpointSourcePinned
		discovered.Endpoint = existing.Endpoint
		if strings.TrimSpace(existing.Protocol) != "" {
			discovered.Protocol = existing.Protocol
		}
		if strings.TrimSpace(existing.Reachability) != "" {
			discovered.Reachability = existing.Reachability
		}
		if strings.TrimSpace(existing.Model) != "" {
			discovered.Model = existing.Model
		}
		if existing.Managed {
			discovered.Managed = true
		}
		if strings.TrimSpace(existing.Metadata[fleet.MetadataChatPath]) != "" {
			discovered.Metadata[fleet.MetadataChatPath] = existing.Metadata[fleet.MetadataChatPath]
		}
		if strings.TrimSpace(existing.Metadata[fleet.MetadataModel]) != "" {
			discovered.Metadata[fleet.MetadataModel] = existing.Metadata[fleet.MetadataModel]
		}
		if strings.TrimSpace(existing.Metadata[fleet.MetadataDefaultModel]) != "" {
			discovered.Metadata[fleet.MetadataDefaultModel] = existing.Metadata[fleet.MetadataDefaultModel]
		}
	}
	return fleet.NormalizeAgentRecord(discovered)
}

func mergeAgentMetadata(existing, discovered map[string]string) map[string]string {
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

func writeFileAtomically(path string, payload []byte) error {
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

	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	ok = true
	return nil
}
