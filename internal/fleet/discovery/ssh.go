package discovery

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"agscale/internal/fleet"
)

func ScanSSHHosts(hosts []string, sshConfig string, ports []int, opts fleet.ProbeOptions) []fleet.AgentRecord {
	timeout := 3
	if opts.TimeoutSeconds > 0 {
		timeout = opts.TimeoutSeconds
	}

	out := []fleet.AgentRecord{}
	for _, host := range hosts {
		h := strings.TrimSpace(host)
		if h == "" {
			continue
		}
		lines := runSSHProbe(h, sshConfig, ports, timeout)
		now := time.Now().UTC()
		for _, line := range lines {
			parts := strings.SplitN(line, "|", 4)
			if len(parts) != 4 || parts[0] != "FOUND" {
				continue
			}
			proto := parts[1]
			endpoint := parts[2]
			lastErr := parts[3]
			health := fleet.HealthHealthy
			if lastErr != "" {
				health = fleet.HealthDegraded
			}
			rec := fleet.AgentRecord{
				ID:             stableID(h, proto, endpoint),
				Name:           inferName(h, proto, endpoint),
				Host:           h,
				HostID:         fleet.StableHostID(h),
				Source:         "ssh-scan",
				Protocol:       proto,
				Endpoint:       endpoint,
				EndpointSource: fleet.EndpointSourceDiscovered,
				Reachability:   fleet.ReachabilitySSHLoopback,
				Health:         health,
				Runtime:        "ssh",
				LastSeenAt:     now,
				LastError:      lastErr,
				Labels: map[string]string{
					"scope": "ssh",
					"host":  h,
				},
				Resources: fleet.ResourceSnapshot{
					Ports: portsForEndpoint(endpoint),
				},
			}
			if proto == "http" {
				rec.Capabilities = []string{"chat", "invoke"}
				rec.Engine = "chat-http"
			}
			if proto == "mcp" {
				rec.Capabilities = []string{"mcp-invoke"}
				rec.Engine = "mcp"
			}
			out = append(out, fleet.RememberDiscoveredAgent(rec))
		}
	}
	return out
}

func CheckSSHHost(host, sshConfig string, timeout int) error {
	target := strings.TrimSpace(host)
	if target == "" {
		return fmt.Errorf("empty host")
	}
	if timeout <= 0 {
		timeout = 5
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", timeout),
	}
	if strings.TrimSpace(sshConfig) != "" {
		args = append(args, "-F", sshConfig)
	}
	args = append(args, target, "sh", "-lc", "hostname >/dev/null && echo ok")
	cmd := exec.Command("ssh", args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh probe failed: %s", sanitizeLine(string(b)))
	}
	if strings.TrimSpace(string(b)) == "" {
		return fmt.Errorf("ssh probe failed: empty response")
	}
	return nil
}

func runSSHProbe(host, sshConfig string, ports []int, timeout int) []string {
	if len(ports) == 0 {
		return nil
	}
	portTokens := make([]string, 0, len(ports))
	for _, p := range ports {
		portTokens = append(portTokens, fmt.Sprintf("%d", p))
	}
	script := fmt.Sprintf(`
status() {
  code="$(curl -s -o /dev/null -w '%%{http_code}' -m %d "$1" 2>/dev/null)"
  rc=$?
  if [ $rc -ne 0 ]; then
    echo 000
    return
  fi
  echo "$code"
}
is_http_endpoint() {
  case "$1" in
    2*|401|403|405) return 0 ;;
    *) return 1 ;;
  esac
}
is_openai_models() {
  case "$1" in
    2*|401|403) return 0 ;;
    *) return 1 ;;
  esac
}
for p in %s; do
  ep="http://127.0.0.1:${p}"
  chat_code="$(status "$ep/chat")"
  invoke_code="$(status "$ep/invoke")"
  models_code="$(status "$ep/v1/models")"
  if is_http_endpoint "$chat_code" || is_http_endpoint "$invoke_code" || is_openai_models "$models_code" ; then
    echo "FOUND|http|$ep|"
    continue
  fi
  mcp_health_code="$(status "$ep/mcp/health")"
  mcp_invoke_code="$(status "$ep/mcp/invoke")"
  if { [ "$mcp_health_code" -ge 200 ] && [ "$mcp_health_code" -lt 300 ]; } || is_http_endpoint "$mcp_invoke_code" ; then
    echo "FOUND|mcp|$ep|"
    continue
  fi
done
`, timeout, strings.Join(portTokens, " "))

	args := []string{"-o", "BatchMode=yes"}
	if strings.TrimSpace(sshConfig) != "" {
		args = append(args, "-F", sshConfig)
	}
	args = append(args, host, "sh", "-lc", script)
	cmd := exec.Command("ssh", args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return []string{fmt.Sprintf("FOUND|unknown|ssh://%s|ssh probe failed: %s", host, sanitizeLine(string(b)))}
	}

	lines := []string{}
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			lines = append(lines, t)
		}
	}
	return lines
}

func sanitizeLine(v string) string {
	s := strings.TrimSpace(v)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", " ")
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
