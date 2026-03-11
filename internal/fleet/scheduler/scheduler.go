package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"agscale/internal/fleet"
)

type Step struct {
	Name     string
	AgentID  string
	Depends  []string
	StepType string
}

type StepResult struct {
	Name       string         `json:"name"`
	AgentID    string         `json:"agentId"`
	Status     string         `json:"status"`
	DurationMs int64          `json:"durationMs,omitempty"`
	Error      string         `json:"error,omitempty"`
	Output     map[string]any `json:"output,omitempty"`
}

type RunOptions struct {
	Topology       string
	TimeoutSeconds int
	DryRun         bool
}

func BuildPlan(agents []fleet.AgentRecord, topology string) []Step {
	if len(agents) == 0 {
		return nil
	}
	if topology != "dag" {
		out := make([]Step, 0, len(agents))
		for i, a := range agents {
			s := Step{
				Name:     fmt.Sprintf("step-%02d", i+1),
				AgentID:  a.ID,
				StepType: "Prompt",
			}
			if i > 0 {
				s.Depends = []string{out[i-1].Name}
			}
			out = append(out, s)
		}
		return out
	}

	steps := []Step{{Name: "triage", AgentID: agents[0].ID, StepType: "Prompt"}}
	if len(agents) == 1 {
		return steps
	}
	retrieve := Step{Name: "retrieve", AgentID: agents[min(1, len(agents)-1)].ID, StepType: "Retrieve", Depends: []string{"triage"}}
	steps = append(steps, retrieve)
	if len(agents) == 2 {
		steps = append(steps, Step{Name: "response", AgentID: agents[1].ID, StepType: "Transform", Depends: []string{"retrieve"}})
		return steps
	}

	if len(agents) >= 4 {
		steps = append(steps, Step{Name: "solve", AgentID: agents[2].ID, StepType: "Transform", Depends: []string{"triage", "retrieve"}})
	}
	lastIdx := len(agents) - 1
	deps := []string{"retrieve"}
	if len(agents) >= 4 {
		deps = []string{"solve"}
	}
	for i := 3; i < lastIdx; i++ {
		name := fmt.Sprintf("specialist-%02d", i-2)
		steps = append(steps, Step{Name: name, AgentID: agents[i].ID, StepType: "ToolCall", Depends: []string{"triage", "retrieve"}})
		deps = append(deps, name)
	}
	steps = append(steps, Step{Name: "response", AgentID: agents[lastIdx].ID, StepType: "Transform", Depends: deps})
	return steps
}

func Execute(task string, agentsByID map[string]fleet.AgentRecord, plan []Step, opts RunOptions) ([]StepResult, error) {
	if len(plan) == 0 {
		return nil, fmt.Errorf("empty plan")
	}
	resultsByName := map[string]StepResult{}
	completed := map[string]bool{}

	for len(completed) < len(plan) {
		progress := false
		for _, step := range plan {
			if completed[step.Name] {
				continue
			}
			ready := true
			for _, dep := range step.Depends {
				if !completed[dep] {
					ready = false
					break
				}
				if r := resultsByName[dep]; r.Status != "Succeeded" {
					resultsByName[step.Name] = StepResult{Name: step.Name, AgentID: step.AgentID, Status: "Skipped", Error: "dependency failed"}
					completed[step.Name] = true
					progress = true
					ready = false
					break
				}
			}
			if !ready || completed[step.Name] {
				continue
			}

			agent, ok := agentsByID[step.AgentID]
			if !ok {
				resultsByName[step.Name] = StepResult{Name: step.Name, AgentID: step.AgentID, Status: "Failed", Error: "agent not found in inventory"}
				completed[step.Name] = true
				progress = true
				continue
			}

			if opts.DryRun {
				resultsByName[step.Name] = StepResult{Name: step.Name, AgentID: step.AgentID, Status: "Succeeded", Output: map[string]any{"dryRun": true}}
				completed[step.Name] = true
				progress = true
				continue
			}

			input := map[string]any{"task": task, "deps": map[string]any{}}
			depMap := input["deps"].(map[string]any)
			for _, dep := range step.Depends {
				depMap[dep] = resultsByName[dep].Output
			}

			start := time.Now()
			out, err := invoke(agent, step, input, opts.TimeoutSeconds)
			result := StepResult{Name: step.Name, AgentID: step.AgentID, DurationMs: time.Since(start).Milliseconds()}
			if err != nil {
				result.Status = "Failed"
				result.Error = err.Error()
			} else {
				result.Status = "Succeeded"
				result.Output = out
			}
			resultsByName[step.Name] = result
			completed[step.Name] = true
			progress = true
		}
		if !progress {
			break
		}
	}

	out := make([]StepResult, 0, len(plan))
	for _, step := range plan {
		out = append(out, resultsByName[step.Name])
	}
	for _, r := range out {
		if r.Status == "Failed" {
			return out, fmt.Errorf("step %s failed: %s", r.Name, r.Error)
		}
	}
	return out, nil
}

func invoke(agent fleet.AgentRecord, step Step, input map[string]any, timeoutSec int) (map[string]any, error) {
	if timeoutSec <= 0 {
		timeoutSec = 20
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	body, _ := json.Marshal(map[string]any{
		"task":     input["task"],
		"step":     step.Name,
		"stepType": step.StepType,
		"input":    input,
	})

	endpoints := resolveEndpoints(agent)
	var lastErr error
	for _, ep := range endpoints {
		req, err := http.NewRequest(http.MethodPost, ep, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			out := map[string]any{}
			if len(b) > 0 && json.Unmarshal(b, &out) != nil {
				out["raw"] = string(b)
			}
			if len(out) == 0 {
				out["ok"] = true
			}
			return out, nil
		}
		lastErr = fmt.Errorf("%s returned http %d", ep, resp.StatusCode)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("invoke failed")
	}
	return nil, lastErr
}

func resolveEndpoints(agent fleet.AgentRecord) []string {
	base := strings.TrimSuffix(agent.Endpoint, "/")
	if agent.Protocol == "mcp" {
		return []string{base + "/mcp/invoke"}
	}
	return []string{base + "/chat", base + "/invoke", base + "/v1/chat/completions"}
}

func RankAgents(in []fleet.AgentRecord, requireSkills []string, maxAgents int) []fleet.AgentRecord {
	type score struct {
		agent     fleet.AgentRecord
		skillHits int
	}
	tmp := make([]score, 0, len(in))
	for _, a := range in {
		hits, ok := skillMatch(a, requireSkills)
		if !ok {
			continue
		}
		tmp = append(tmp, score{agent: a, skillHits: hits})
	}
	sort.Slice(tmp, func(i, j int) bool {
		if tmp[i].skillHits != tmp[j].skillHits {
			return tmp[i].skillHits > tmp[j].skillHits
		}
		if tmp[i].agent.Health != tmp[j].agent.Health {
			return tmp[i].agent.Health == fleet.HealthHealthy
		}
		if tmp[i].agent.LatencyMs != tmp[j].agent.LatencyMs {
			return tmp[i].agent.LatencyMs < tmp[j].agent.LatencyMs
		}
		return tmp[i].agent.Name < tmp[j].agent.Name
	})
	out := make([]fleet.AgentRecord, 0, len(tmp))
	for _, s := range tmp {
		out = append(out, s.agent)
		if maxAgents > 0 && len(out) >= maxAgents {
			break
		}
	}
	return out
}

func skillMatch(a fleet.AgentRecord, required []string) (int, bool) {
	if len(required) == 0 {
		return len(a.Skills), true
	}
	have := map[string]bool{}
	for _, s := range a.Skills {
		have[s] = true
	}
	hits := 0
	for _, r := range required {
		if !have[r] {
			return 0, false
		}
		hits++
	}
	return hits, true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
