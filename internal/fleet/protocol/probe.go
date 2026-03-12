package protocol

import (
	"encoding/json"
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
	httpProbe := probeHTTP(client, endpoint)
	if httpProbe.Healthy {
		return fleet.ProbeResult{
			Healthy:      true,
			Protocol:     "http",
			LatencyMs:    time.Since(start).Milliseconds(),
			Capabilities: httpProbe.Capabilities,
			Metadata:     httpProbe.Metadata,
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

func probeHTTP(client *http.Client, base string) fleet.ProbeResult {
	capabilities := []string{}
	metadata := map[string]string{}

	if code, body := request(client, http.MethodGet, joinURL(base, "/v1/models"), nil); isOpenAIModelsStatus(code) {
		capabilities = append(capabilities, "openai-chat")
		metadata["chatPath"] = "/v1/chat/completions"
		if model := firstOpenAIModel(body); model != "" {
			metadata["model"] = model
		}
	}

	if code, _ := request(client, http.MethodGet, joinURL(base, "/chat"), nil); isInteractiveEndpointStatus(code) {
		capabilities = append(capabilities, "chat")
		metadata["chatPath"] = "/chat"
	}

	if code, _ := request(client, http.MethodGet, joinURL(base, "/invoke"), nil); isInteractiveEndpointStatus(code) {
		capabilities = append(capabilities, "invoke")
	}

	if len(capabilities) == 0 {
		return fleet.ProbeResult{}
	}
	if len(metadata) == 0 {
		metadata = nil
	}
	return fleet.ProbeResult{
		Healthy:      true,
		Protocol:     "http",
		Capabilities: capabilities,
		Metadata:     metadata,
	}
}

func probeMCP(client *http.Client, base string) bool {
	healthCode, _ := request(client, http.MethodGet, joinURL(base, "/mcp/health"), nil)
	if healthCode >= 200 && healthCode < 300 {
		return true
	}
	invokeCode, _ := request(client, http.MethodGet, joinURL(base, "/mcp/invoke"), nil)
	return isInteractiveEndpointStatus(invokeCode)
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
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func joinURL(base, path string) string {
	b := strings.TrimSuffix(base, "/")
	p := path
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return fmt.Sprintf("%s%s", b, p)
}

func isInteractiveEndpointStatus(code int) bool {
	return (code >= 200 && code < 300) || code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusMethodNotAllowed
}

func isOpenAIModelsStatus(code int) bool {
	return (code >= 200 && code < 300) || code == http.StatusUnauthorized || code == http.StatusForbidden
}

func firstOpenAIModel(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	if len(payload.Data) == 0 {
		return ""
	}
	return strings.TrimSpace(payload.Data[0].ID)
}
