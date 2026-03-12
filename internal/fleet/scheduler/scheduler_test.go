package scheduler

import (
	"encoding/json"
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
