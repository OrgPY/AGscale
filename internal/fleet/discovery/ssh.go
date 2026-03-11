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
			parts := strings.Split(line, "|")
			if len(parts) < 4 || parts[0] != "FOUND" {
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
				ID:         stableID(h, proto, endpoint),
				Name:       inferName(h, proto, endpoint),
				Host:       h,
				Source:     "ssh-scan",
				Protocol:   proto,
				Endpoint:   endpoint,
				Health:     health,
				LastSeenAt: now,
				LastError:  lastErr,
				Labels: map[string]string{
					"scope": "ssh",
					"host":  h,
				},
			}
			if proto == "http" {
				rec.Capabilities = []string{"chat", "invoke"}
			}
			if proto == "mcp" {
				rec.Capabilities = []string{"mcp-invoke"}
			}
			out = append(out, rec)
		}
	}
	return out
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
for p in %s; do
  ep="http://127.0.0.1:${p}"
  if curl -fsS -m %d "$ep/healthz" >/dev/null 2>&1 || curl -fsS -m %d "$ep/health" >/dev/null 2>&1 || curl -fsS -m %d "$ep/v1/models" >/dev/null 2>&1 ; then
    echo "FOUND|http|$ep|"
    continue
  fi
  if curl -fsS -m %d "$ep/mcp/health" >/dev/null 2>&1 || curl -fsS -m %d "$ep/mcp/invoke" >/dev/null 2>&1 ; then
    echo "FOUND|mcp|$ep|"
    continue
  fi
done
`, strings.Join(portTokens, " "), timeout, timeout, timeout, timeout, timeout)

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
