package discovery

import (
	"reflect"
	"strings"
	"testing"

	"agscale/internal/fleet"
)

func TestExtractPortsFromCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  string
		want []int
	}{
		{name: "equals form", cmd: "agent --port=8080", want: []int{8080}},
		{name: "separate form", cmd: "agent --port 8081", want: []int{8081}},
		{name: "short flag", cmd: "agent -p 8082", want: []int{8082}},
		{name: "address token", cmd: "agent 127.0.0.1:11434", want: []int{11434}},
		{name: "deduplicated", cmd: "agent --port=8080 127.0.0.1:8080", want: []int{8080}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := extractPortsFromCommand(tc.cmd); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expected ports %v, got %v", tc.want, got)
			}
		})
	}
}

func TestParsePortFromAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  int
	}{
		{input: "127.0.0.1:8080", want: 8080},
		{input: "TCP 127.0.0.1:18789 (LISTEN)", want: 18789},
		{input: "localhost:notaport", want: 0},
		{input: "", want: 0},
	}

	for _, tc := range tests {
		if got := parsePortFromAddr(tc.input); got != tc.want {
			t.Fatalf("parsePortFromAddr(%q): expected %d, got %d", tc.input, tc.want, got)
		}
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{input: "http://127.0.0.1:8080/", want: "http://127.0.0.1:8080"},
		{input: "", want: ""},
	}

	for _, tc := range tests {
		if got := normalizeEndpoint(tc.input); got != tc.want {
			t.Fatalf("normalizeEndpoint(%q): expected %q, got %q", tc.input, tc.want, got)
		}
	}
}

func TestInferEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		probe fleet.ProbeResult
		want  string
	}{
		{
			name: "openai compatible",
			probe: fleet.ProbeResult{
				Metadata: map[string]string{"model": "gpt-test"},
			},
			want: "openai-compatible",
		},
		{
			name: "chat http",
			probe: fleet.ProbeResult{
				Metadata: map[string]string{"chatPath": "/chat"},
			},
			want: "chat-http",
		},
		{
			name: "mcp",
			probe: fleet.ProbeResult{
				Capabilities: []string{"mcp-invoke"},
			},
			want: "mcp",
		},
		{
			name:  "unknown",
			probe: fleet.ProbeResult{},
			want:  "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := inferEngine(tc.probe); got != tc.want {
				t.Fatalf("expected engine %q, got %q", tc.want, got)
			}
		})
	}
}

func TestLocalDefaults(t *testing.T) {
	t.Parallel()

	ports := DefaultAgentPorts()
	if !reflect.DeepEqual(ports, []int{18789, 8080, 8000, 3000, 5000, 11434}) {
		t.Fatalf("unexpected default ports %v", ports)
	}

	endpoints := LocalDefaultEndpoints()
	if len(endpoints) != len(ports) {
		t.Fatalf("expected %d default endpoints, got %d", len(ports), len(endpoints))
	}
	if !strings.HasPrefix(endpoints[0], "http://127.0.0.1:") {
		t.Fatalf("expected localhost endpoint, got %q", endpoints[0])
	}

	signatures := DefaultAgentSignatures()
	if len(signatures) == 0 || signatures[0] != "openclaw" {
		t.Fatalf("unexpected default signatures %v", signatures)
	}
}
