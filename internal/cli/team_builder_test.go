package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunTeamValidateReturnsErrorOnServerDryRunFailure(t *testing.T) {
	oldExecCommand := execCommand
	execCommand = fakeKubectlExecCommand
	defer func() { execCommand = oldExecCommand }()

	bundlePath := t.TempDir() + "/bundle.yaml"
	if err := os.WriteFile(bundlePath, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"), 0644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	err := runTeamValidate([]string{"-f", bundlePath, "-n", "default"})
	if err == nil {
		t.Fatalf("expected server dry-run failure to return an error")
	}
	if !strings.Contains(err.Error(), "server dry-run failed") {
		t.Fatalf("expected server dry-run error, got %v", err)
	}
}

func TestTeamBuilderHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_TEAM_BUILDER_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	dash := -1
	for i, arg := range args {
		if arg == "--" {
			dash = i
			break
		}
	}
	if dash == -1 || dash+1 >= len(args) {
		os.Exit(2)
	}

	cmdArgs := args[dash+1:]
	if len(cmdArgs) == 0 || cmdArgs[0] != "kubectl" {
		os.Exit(2)
	}

	joined := strings.Join(cmdArgs[1:], " ")
	switch {
	case strings.Contains(joined, "--dry-run=client"):
		_, _ = os.Stdout.WriteString("client ok")
		os.Exit(0)
	case strings.Contains(joined, "--dry-run=server"):
		_, _ = os.Stdout.WriteString("server rejected")
		os.Exit(1)
	default:
		os.Exit(0)
	}
}

func fakeKubectlExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestTeamBuilderHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_TEAM_BUILDER_HELPER_PROCESS=1")
	return cmd
}
