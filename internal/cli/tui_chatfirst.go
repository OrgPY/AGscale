package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	agscalev1alpha1 "agscale/api/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type tuiChatLine struct {
	role string
	text string
	raw  string
	meta string
}

type tuiPortForwardSession struct {
	url     string
	cleanup func()
}

func (s *tuiPortForwardSession) close() {
	if s.cleanup != nil {
		s.cleanup()
	}
	s.url = ""
	s.cleanup = nil
}

func runAgentTUIChatFirst(client ctrlclient.Client, kc *kubernetes.Clientset, namespace, agentName string) error {
	reader := bufio.NewReader(os.Stdin)
	ctx := context.Background()
	pf := &tuiPortForwardSession{}
	defer pf.close()

	agent := &agscalev1alpha1.Agent{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: agentName}, agent); err != nil {
		return err
	}

	transcript := []tuiChatLine{{
		role: "system",
		text: "Chat-first mode active. Type directly to chat, or use /help for actions.",
	}}
	showRaw := false
	info := ""

	for {
		renderChatFirstScreen(agent, transcript, info, showRaw)
		fmt.Print("chat> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			cmd, arg := splitCommand(input)
			switch cmd {
			case "/q", "/quit", "/exit":
				return nil
			case "/help":
				info = "Commands: /refresh /logs /restart /scale <n> /failover /raw /quit"
			case "/raw":
				showRaw = !showRaw
				info = fmt.Sprintf("raw-json view: %t", showRaw)
			case "/refresh":
				newAgent := &agscalev1alpha1.Agent{}
				if err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: agentName}, newAgent); err != nil {
					info = "refresh failed: " + err.Error()
				} else {
					agent = newAgent
					info = "refreshed"
				}
			case "/logs":
				pod, err := firstAgentPod(ctx, kc, namespace, agentName)
				if err != nil {
					info = "logs failed: " + err.Error()
					continue
				}
				var buf bytes.Buffer
				if err := streamPodLogs(ctx, kc, namespace, pod.Name, 100, &buf); err != nil {
					info = "logs failed: " + err.Error()
					continue
				}
				fmt.Print("\033[2J\033[H")
				fmt.Println("=== Recent Logs ===")
				fmt.Println(strings.TrimSpace(buf.String()))
				fmt.Print("\npress Enter to return...")
				_, _ = reader.ReadString('\n')
				info = "logs shown"
			case "/restart":
				if !confirm(reader, "Restart one pod? [y/N]: ") {
					info = "restart cancelled"
					continue
				}
				if err := restartAgentPod(ctx, kc, namespace, agentName); err != nil {
					info = "restart failed: " + err.Error()
				} else {
					info = "restart triggered"
				}
			case "/scale":
				if arg == "" {
					info = "usage: /scale <replicas>"
					continue
				}
				v, err := strconv.Atoi(arg)
				if err != nil || v < 1 {
					info = "invalid replicas value"
					continue
				}
				if err := scaleAgent(ctx, client, namespace, agentName, int32(v)); err != nil {
					info = "scale failed: " + err.Error()
				} else {
					info = fmt.Sprintf("scaled to %d", v)
				}
			case "/failover":
				if !confirm(reader, "Switch to backup LLM endpoint? [y/N]: ") {
					info = "failover cancelled"
					continue
				}
				if err := failoverAgentLLM(ctx, client, namespace, agentName); err != nil {
					info = "failover failed: " + err.Error()
				} else {
					info = "failover triggered"
				}
			default:
				info = "unknown command, try /help"
			}
			continue
		}

		transcript = append(transcript, tuiChatLine{role: "user", text: input})
		resp, err := sendAgentChatMessage(agent, input, pf)
		if err != nil {
			transcript = append(transcript, tuiChatLine{
				role: "system",
				text: "chat error: " + err.Error(),
			})
			info = "chat failed"
			continue
		}
		transcript = append(transcript, tuiChatLine{
			role: "assistant",
			text: resp.text,
			raw:  resp.raw,
			meta: fmt.Sprintf("status=%d latency=%s", resp.statusCode, resp.latency.Round(10*time.Millisecond)),
		})
		info = "chat ok"
	}
}

type chatResponse struct {
	text       string
	raw        string
	statusCode int
	latency    time.Duration
}

func sendAgentChatMessage(agent *agscalev1alpha1.Agent, prompt string, pf *tuiPortForwardSession) (chatResponse, error) {
	baseURL, path, err := resolveChatEndpoint(agent, pf)
	if err != nil {
		return chatResponse{}, err
	}

	payload := map[string]string{"message": prompt}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token, ok := chatTokenFromEnv(agent.Name); ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	start := time.Now()
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return chatResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return chatResponse{
		text:       extractMessage(strings.TrimSpace(string(raw))),
		raw:        strings.TrimSpace(string(raw)),
		statusCode: resp.StatusCode,
		latency:    time.Since(start),
	}, nil
}

func resolveChatEndpoint(agent *agscalev1alpha1.Agent, pf *tuiPortForwardSession) (baseURL string, path string, err error) {
	if agent == nil || agent.Status.URL == "" {
		return "", "", fmt.Errorf("agent status url empty")
	}
	path = agent.Annotations["agscale.io/chat-path"]
	if path == "" {
		path = "/chat"
	}
	baseURL = strings.TrimSuffix(agent.Status.URL, "/")
	if override, ok := chatBaseURLFromEnv(agent.Name); ok {
		baseURL = strings.TrimSuffix(override, "/")
	}

	if isClusterLocalURL(baseURL) {
		if pf.url == "" {
			localURL, cleanup, err := startServicePortForward(agent.Namespace, agent.Name)
			if err != nil {
				return "", "", fmt.Errorf("auto port-forward failed: %w", err)
			}
			pf.url = localURL
			pf.cleanup = cleanup
		}
		baseURL = pf.url
	}
	return baseURL, path, nil
}

func extractMessage(raw string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return raw
	}
	if choices, ok := obj["choices"].([]any); ok && len(choices) > 0 {
		if c0, ok := choices[0].(map[string]any); ok {
			if msg, ok := c0["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok && content != "" {
					return content
				}
			}
		}
	}
	if v, ok := obj["reply"].(string); ok && v != "" {
		return v
	}
	if v, ok := obj["message"].(string); ok && v != "" {
		return v
	}
	return raw
}

func renderChatFirstScreen(agent *agscalev1alpha1.Agent, transcript []tuiChatLine, info string, showRaw bool) {
	fmt.Print("\033[2J\033[H")
	fmt.Println("┌─────────────────────────────────────────────────────────────────────────────┐")
	fmt.Printf("│ AGScale TUI  %s/%s", agent.Namespace, agent.Name)
	padding := 76 - len(agent.Namespace) - len(agent.Name) - len(" AGScale TUI  /")
	if padding < 1 {
		padding = 1
	}
	fmt.Print(strings.Repeat(" ", padding))
	fmt.Println("│")
	fmt.Println("├─────────────────────────────────────────────────────────────────────────────┤")
	fmt.Printf("│ ReadyReplicas: %-3d  Budget: %-10s  URL: %-30s │\n", agent.Status.ReadyReplicas, safe(agent.Status.BudgetPhase), truncate(agent.Status.URL, 30))
	fmt.Println("├─────────────────────────────────────────────────────────────────────────────┤")
	fmt.Println("│ Chat (direct input)                                                        │")

	lines := transcript
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	for _, line := range lines {
		role := "SYS"
		switch line.role {
		case "user":
			role = "YOU"
		case "assistant":
			role = "BOT"
		}
		content := line.text
		if showRaw && line.raw != "" {
			content = line.raw
		}
		msg := truncate(strings.ReplaceAll(content, "\n", " "), 68)
		fmt.Printf("│ %-3s: %-68s │\n", role, msg)
		if line.meta != "" {
			fmt.Printf("│      %-68s │\n", truncate(line.meta, 68))
		}
	}
	fmt.Println("├─────────────────────────────────────────────────────────────────────────────┤")
	fmt.Printf("│ Options: /refresh /logs /restart /scale <n> /failover /raw /quit          │\n")
	fmt.Printf("│ Mode: %-12s  Info: %-54s │\n", map[bool]string{true: "raw-json", false: "smart-text"}[showRaw], truncate(info, 54))
	fmt.Println("└─────────────────────────────────────────────────────────────────────────────┘")
}

func splitCommand(input string) (cmd, arg string) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", ""
	}
	cmd = strings.ToLower(fields[0])
	if len(fields) > 1 {
		arg = strings.Join(fields[1:], " ")
	}
	return cmd, arg
}

func confirm(reader *bufio.Reader, prompt string) bool {
	fmt.Print(prompt)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func truncate(s string, max int) string {
	if max <= 3 || len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func safe(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
