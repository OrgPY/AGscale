package scheduler

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"agscale/internal/fleet"
)

func TestResolveEndpointsPrefersDiscoveredChatPath(t *testing.T) {
	t.Parallel()

	agent := fleet.AgentRecord{
		Endpoint: "http://127.0.0.1:8080",
		Metadata: map[string]string{"chatPath": "/v1/chat/completions"},
	}

	endpoints := resolveEndpoints(agent)
	if len(endpoints) == 0 || endpoints[0] != "http://127.0.0.1:8080/v1/chat/completions" {
		t.Fatalf("expected discovered chat path to be first, got %v", endpoints)
	}
}

func TestBuildInvokeRequestBodyOpenAICompatible(t *testing.T) {
	t.Parallel()

	agent := fleet.AgentRecord{
		ID:   "agent-1",
		Name: "agent-1",
		Metadata: map[string]string{
			"model": "demo-model",
		},
	}
	step := Step{Name: "triage", StepType: "Prompt"}

	body, err := buildInvokeRequestBody(agent, step, map[string]any{
		"task": "incident-401",
		"deps": map[string]any{},
	}, "http://agent.example/v1/chat/completions")
	if err != nil {
		t.Fatalf("buildInvokeRequestBody returned error: %v", err)
	}

	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if payload.Model != "demo-model" {
		t.Fatalf("expected discovered model to be used, got %q", payload.Model)
	}
	if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" {
		t.Fatalf("unexpected openai messages payload: %+v", payload.Messages)
	}
	if !strings.Contains(payload.Messages[0].Content, `"task":"incident-401"`) {
		t.Fatalf("expected task payload to be embedded in openai content, got %q", payload.Messages[0].Content)
	}
}

func TestRankAgentsPrefersHostDiversityWithoutDuplicates(t *testing.T) {
	t.Parallel()

	agents := []fleet.AgentRecord{
		{ID: "a1", Name: "agent-1", Host: "host-a", HostID: "host-a", Health: fleet.HealthHealthy, LatencyMs: 5},
		{ID: "a2", Name: "agent-2", Host: "host-a", HostID: "host-a", Health: fleet.HealthHealthy, LatencyMs: 10},
		{ID: "b1", Name: "agent-3", Host: "host-b", HostID: "host-b", Health: fleet.HealthHealthy, LatencyMs: 15},
	}

	selected := RankAgents(agents, nil, 2)
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected agents, got %d", len(selected))
	}
	if selected[0].ID == selected[1].ID {
		t.Fatalf("expected unique agents, got duplicate %q", selected[0].ID)
	}
	if selected[0].HostID == selected[1].HostID {
		t.Fatalf("expected host diversity, got same host %q", selected[0].HostID)
	}
}

func TestBuildPlanDAGVariants(t *testing.T) {
	t.Parallel()

	agents := []fleet.AgentRecord{
		{ID: "a1"},
		{ID: "a2"},
		{ID: "a3"},
		{ID: "a4"},
		{ID: "a5"},
	}
	tests := []struct {
		name  string
		count int
		want  []Step
	}{
		{
			name:  "one agent",
			count: 1,
			want: []Step{
				{Name: "triage", AgentID: "a1", StepType: "Prompt"},
			},
		},
		{
			name:  "two agents",
			count: 2,
			want: []Step{
				{Name: "triage", AgentID: "a1", StepType: "Prompt"},
				{Name: "retrieve", AgentID: "a2", StepType: "Retrieve", Depends: []string{"triage"}},
				{Name: "response", AgentID: "a2", StepType: "Transform", Depends: []string{"retrieve"}},
			},
		},
		{
			name:  "three agents",
			count: 3,
			want: []Step{
				{Name: "triage", AgentID: "a1", StepType: "Prompt"},
				{Name: "retrieve", AgentID: "a2", StepType: "Retrieve", Depends: []string{"triage"}},
				{Name: "response", AgentID: "a3", StepType: "Transform", Depends: []string{"triage", "retrieve"}},
			},
		},
		{
			name:  "four agents",
			count: 4,
			want: []Step{
				{Name: "triage", AgentID: "a1", StepType: "Prompt"},
				{Name: "retrieve", AgentID: "a2", StepType: "Retrieve", Depends: []string{"triage"}},
				{Name: "solve", AgentID: "a3", StepType: "Transform", Depends: []string{"triage", "retrieve"}},
				{Name: "response", AgentID: "a4", StepType: "Transform", Depends: []string{"solve"}},
			},
		},
		{
			name:  "five agents",
			count: 5,
			want: []Step{
				{Name: "triage", AgentID: "a1", StepType: "Prompt"},
				{Name: "retrieve", AgentID: "a2", StepType: "Retrieve", Depends: []string{"triage"}},
				{Name: "solve", AgentID: "a3", StepType: "Transform", Depends: []string{"triage", "retrieve"}},
				{Name: "specialist-01", AgentID: "a4", StepType: "ToolCall", Depends: []string{"triage", "retrieve"}},
				{Name: "response", AgentID: "a5", StepType: "Transform", Depends: []string{"solve", "specialist-01"}},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := BuildPlan(agents[:tc.count], "dag")
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected dag plan\nwant: %#v\ngot:  %#v", tc.want, got)
			}
		})
	}
}

func TestBuildPlanSequential(t *testing.T) {
	t.Parallel()

	got := BuildPlan([]fleet.AgentRecord{{ID: "a1"}, {ID: "a2"}, {ID: "a3"}}, "sequential")
	want := []Step{
		{Name: "step-01", AgentID: "a1", StepType: "Prompt"},
		{Name: "step-02", AgentID: "a2", StepType: "Prompt", Depends: []string{"step-01"}},
		{Name: "step-03", AgentID: "a3", StepType: "Prompt", Depends: []string{"step-02"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected sequential plan\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestExecuteDryRun(t *testing.T) {
	t.Parallel()

	results, err := Execute("incident", map[string]fleet.AgentRecord{
		"a1": {ID: "a1"},
	}, []Step{{
		Name:     "triage",
		AgentID:  "a1",
		StepType: "Prompt",
	}}, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(results) != 1 || results[0].Status != "Succeeded" {
		t.Fatalf("unexpected dry-run results %+v", results)
	}
	if dryRun, ok := results[0].Output["dryRun"].(bool); !ok || !dryRun {
		t.Fatalf("expected dryRun output marker, got %+v", results[0].Output)
	}
}

func TestExecuteSkipsDependencyAfterFailure(t *testing.T) {
	t.Parallel()

	plan := []Step{
		{Name: "triage", AgentID: "missing", StepType: "Prompt"},
		{Name: "response", AgentID: "a2", StepType: "Transform", Depends: []string{"triage"}},
	}
	results, err := Execute("incident", map[string]fleet.AgentRecord{
		"a2": {ID: "a2"},
	}, plan, RunOptions{})
	if err == nil {
		t.Fatal("expected Execute to return an error")
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(results))
	}
	if results[0].Status != "Failed" {
		t.Fatalf("expected first step to fail, got %+v", results[0])
	}
	if results[1].Status != "Skipped" {
		t.Fatalf("expected dependent step to be skipped, got %+v", results[1])
	}
}

func TestExecuteMissingAgent(t *testing.T) {
	t.Parallel()

	results, err := Execute("incident", map[string]fleet.AgentRecord{}, []Step{{
		Name:     "triage",
		AgentID:  "missing",
		StepType: "Prompt",
	}}, RunOptions{})
	if err == nil {
		t.Fatal("expected Execute to return an error for missing agent")
	}
	if len(results) != 1 || results[0].Status != "Failed" {
		t.Fatalf("unexpected missing-agent result %+v", results)
	}
}

func TestInvokeReturnsReadError(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       errorReadCloser{},
				Header:     make(http.Header),
			}, nil
		}),
	}

	_, err := invoke(client, fleet.AgentRecord{
		Endpoint: "http://agent.example",
		Protocol: "mcp",
	}, Step{Name: "triage", StepType: "Prompt"}, map[string]any{
		"task": "incident",
		"deps": map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "response read error") {
		t.Fatalf("expected response read error, got %v", err)
	}
}

func TestSkillMatch(t *testing.T) {
	t.Parallel()

	agent := fleet.AgentRecord{Skills: []string{"triage", "search"}}
	if hits, ok := skillMatch(agent, nil); !ok || hits != 2 {
		t.Fatalf("expected empty required skills to match all, got hits=%d ok=%t", hits, ok)
	}
	if hits, ok := skillMatch(agent, []string{"triage", "write"}); ok || hits != 0 {
		t.Fatalf("expected missing skill to fail, got hits=%d ok=%t", hits, ok)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("boom")
}

func (errorReadCloser) Close() error {
	return nil
}
