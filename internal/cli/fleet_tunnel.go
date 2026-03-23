package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"agscale/internal/fleet"
)

var startFleetSSHTunnelFn = startFleetSSHTunnel

const (
	sshTunnelReadyTimeout = 5 * time.Second
	sshTunnelDialTimeout  = 200 * time.Millisecond
	sshTunnelDialInterval = 100 * time.Millisecond
	sshTunnelKillTimeout  = 2 * time.Second
)

type fleetTunnelSession struct {
	key     string
	baseURL string
	cleanup func()
}

func (s *fleetTunnelSession) close() {
	if s.cleanup != nil {
		s.cleanup()
	}
	s.key = ""
	s.baseURL = ""
	s.cleanup = nil
}

type fleetTunnelManager struct {
	sshConfigPath string
	sessions      map[string]*fleetTunnelSession
}

func newFleetTunnelManager(sshConfigPath string) *fleetTunnelManager {
	return &fleetTunnelManager{
		sshConfigPath: expandHome(strings.TrimSpace(sshConfigPath)),
		sessions:      map[string]*fleetTunnelSession{},
	}
}

func (m *fleetTunnelManager) Close() {
	for _, session := range m.sessions {
		session.close()
	}
	m.sessions = map[string]*fleetTunnelSession{}
}

func (m *fleetTunnelManager) PrepareAgent(agent fleet.AgentRecord) (fleet.AgentRecord, error) {
	agent = fleet.NormalizeAgentRecord(agent)
	if !agentNeedsSSHTunnel(agent) {
		return agent, nil
	}

	key := tunnelCacheKey(agent.Host, agent.Endpoint)
	if session, ok := m.sessions[key]; ok {
		agent.Endpoint = session.baseURL
		return agent, nil
	}

	baseURL, cleanup, err := startFleetSSHTunnelFn(agent, m.sshConfigPath)
	if err != nil {
		return agent, fmt.Errorf("agent %s ssh tunnel failed: %w", firstNonEmpty(agent.Name, agent.ID), err)
	}

	session := &fleetTunnelSession{
		key:     key,
		baseURL: baseURL,
		cleanup: cleanup,
	}
	m.sessions[key] = session
	agent.Endpoint = session.baseURL
	return agent, nil
}

func agentNeedsSSHTunnel(agent fleet.AgentRecord) bool {
	agent = fleet.NormalizeAgentRecord(agent)
	return agent.Reachability == fleet.ReachabilitySSHLoopback && !fleet.IsLocalHostValue(firstNonEmpty(agent.Host, agent.HostID))
}

func startFleetSSHTunnel(agent fleet.AgentRecord, sshConfigPath string) (string, func(), error) {
	agent = fleet.NormalizeAgentRecord(agent)
	if !agentNeedsSSHTunnel(agent) {
		return strings.TrimSuffix(agent.Endpoint, "/"), func() {}, nil
	}

	sshTarget := strings.TrimSpace(firstNonEmpty(agent.Host, agent.HostID))
	if sshTarget == "" {
		return "", nil, errors.New("ssh-loopback agent missing host target")
	}

	u, err := url.Parse(strings.TrimSpace(agent.Endpoint))
	if err != nil || u.Hostname() == "" {
		return "", nil, fmt.Errorf("invalid agent endpoint %q", agent.Endpoint)
	}
	remoteHost := u.Hostname()
	remotePort := servicePortFromURL(agent.Endpoint)
	return startSSHTunnel(sshTarget, sshConfigPath, firstNonEmpty(u.Scheme, "http"), remoteHost, remotePort)
}

func startSSHTunnel(sshTarget, sshConfigPath, scheme, remoteHost string, remotePort int) (string, func(), error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	localPort := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	if strings.TrimSpace(sshTarget) == "" {
		return "", nil, errors.New("empty ssh target")
	}
	if strings.TrimSpace(remoteHost) == "" {
		remoteHost = "127.0.0.1"
	}
	if remotePort <= 0 {
		remotePort = 80
	}
	if strings.TrimSpace(scheme) == "" {
		scheme = "http"
	}

	var stderr bytes.Buffer
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
	}
	if cfg := strings.TrimSpace(expandHome(sshConfigPath)); cfg != "" {
		args = append(args, "-F", cfg)
	}
	args = append(args, "-N", "-L", fmt.Sprintf("%d:%s:%d", localPort, remoteHost, remotePort), sshTarget)

	cmd := exec.Command("ssh", args...)
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	ready := false
	deadline := time.Now().Add(sshTunnelReadyTimeout)
	for time.Now().Before(deadline) {
		select {
		case waitErr := <-waitCh:
			if waitErr != nil {
				return "", nil, fmt.Errorf("ssh exited before tunnel ready: %s", sanitizeTunnelError(stderr.String()))
			}
			return "", nil, errors.New("ssh exited before tunnel ready")
		default:
		}

		conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), sshTunnelDialTimeout)
		if dialErr == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(sshTunnelDialInterval)
	}
	if !ready {
		stopSSHProcess(cmd, waitCh)
		return "", nil, errors.New("timed out waiting for ssh tunnel to become ready")
	}

	cleanup := func() {
		stopSSHProcess(cmd, waitCh)
	}
	return fmt.Sprintf("%s://127.0.0.1:%d", scheme, localPort), cleanup, nil
}

func defaultFleetSSHConfig() string {
	if v := strings.TrimSpace(os.Getenv("AGSCALE_FLEET_SSH_CONFIG")); v != "" {
		return v
	}
	return "~/.ssh/config"
}

func tunnelCacheKey(sshTarget, endpoint string) string {
	return strings.TrimSpace(sshTarget) + "|" + strings.TrimSpace(endpoint)
}

func sanitizeTunnelError(v string) string {
	s := strings.TrimSpace(v)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 160 {
		return s[:160]
	}
	return s
}

func stopSSHProcess(cmd *exec.Cmd, waitCh <-chan error) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-waitCh:
	case <-time.After(sshTunnelKillTimeout):
	}
}
