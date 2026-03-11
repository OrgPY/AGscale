package protocol

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"agscale/internal/fleet"
)

func ProbeEndpoint(endpoint string, opts fleet.ProbeOptions) fleet.ProbeResult {
	timeout := 3 * time.Second
	if opts.TimeoutSeconds > 0 {
		timeout = time.Duration(opts.TimeoutSeconds) * time.Second
	}
	client := &http.Client{Timeout: timeout}

	start := time.Now()
	if ok := probeHTTP(client, endpoint); ok {
		return fleet.ProbeResult{
			Healthy:      true,
			Protocol:     "http",
			LatencyMs:    time.Since(start).Milliseconds(),
			Capabilities: []string{"chat", "invoke"},
		}
	}

	start = time.Now()
	if ok := probeMCP(client, endpoint); ok {
		return fleet.ProbeResult{
			Healthy:      true,
			Protocol:     "mcp",
			LatencyMs:    time.Since(start).Milliseconds(),
			Capabilities: []string{"mcp-invoke"},
		}
	}

	return fleet.ProbeResult{
		Healthy:   false,
		Protocol:  "",
		LatencyMs: 0,
		LastError: "no supported agent protocol response",
	}
}

func probeHTTP(client *http.Client, base string) bool {
	paths := []string{"/healthz", "/health", "/v1/models", "/chat", "/invoke"}
	for _, p := range paths {
		code, _ := request(client, http.MethodGet, joinURL(base, p), nil)
		if code >= 200 && code < 500 {
			return true
		}
	}
	return false
}

func probeMCP(client *http.Client, base string) bool {
	paths := []string{"/mcp/health", "/mcp/invoke"}
	for _, p := range paths {
		code, _ := request(client, http.MethodGet, joinURL(base, p), nil)
		if code >= 200 && code < 500 {
			return true
		}
	}
	return false
}

func request(client *http.Client, method, url string, body io.Reader) (int, string) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	return resp.StatusCode, ""
}

func joinURL(base, path string) string {
	b := strings.TrimSuffix(base, "/")
	p := path
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return fmt.Sprintf("%s%s", b, p)
}
