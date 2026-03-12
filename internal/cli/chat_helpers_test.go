package cli

import (
	"encoding/json"
	"testing"
)

func TestServicePortFromURLUsesExplicitPort(t *testing.T) {
	t.Parallel()

	if got := servicePortFromURL("http://agent.default.svc.cluster.local:8081"); got != 8081 {
		t.Fatalf("expected explicit port 8081, got %d", got)
	}
	if got := servicePortFromURL("https://agent.default.svc.cluster.local"); got != 443 {
		t.Fatalf("expected https default port 443, got %d", got)
	}
}

func TestBuildChatRequestBodyOpenAICompatible(t *testing.T) {
	t.Parallel()

	body, err := buildChatRequestBody("demo-agent", "hello world", "/v1/chat/completions", "demo-model")
	if err != nil {
		t.Fatalf("buildChatRequestBody returned error: %v", err)
	}

	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Model != "demo-model" {
		t.Fatalf("expected model demo-model, got %q", payload.Model)
	}
	if len(payload.Messages) != 1 || payload.Messages[0].Content != "hello world" {
		t.Fatalf("unexpected messages payload: %+v", payload.Messages)
	}
}
