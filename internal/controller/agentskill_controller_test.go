package controller

import "testing"

func TestParseOpenAPISkill(t *testing.T) {
	payload := map[string]any{
		"type":        "http-tool",
		"description": "search",
		"endpoint":    "https://hub.example.com/tool/search",
		"version":     "1.2.3",
		"config": map[string]any{
			"timeoutMs": 2000,
		},
	}
	skill, version, err := parseOpenAPISkill(payload)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if skill.Type != "http-tool" || skill.Endpoint == "" || version != "1.2.3" {
		t.Fatalf("unexpected parse result: %#v version=%q", skill, version)
	}
}

func TestParseMCPSkill(t *testing.T) {
	payload := map[string]any{
		"version": "2026-02-13",
		"tool": map[string]any{
			"type":        "mcp-tool",
			"description": "fetch docs",
			"endpoint":    "wss://mcp.example.com/tools/fetch-docs",
			"config": map[string]any{
				"server": "public-mcp",
			},
		},
	}
	skill, version, err := parseMCPSkill(payload)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if skill.Type != "mcp-tool" || skill.Endpoint == "" || version == "" {
		t.Fatalf("unexpected parse result: %#v version=%q", skill, version)
	}
}
