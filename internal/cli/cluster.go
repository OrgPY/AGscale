package cli

import (
	"errors"
	"fmt"
	"os"
)

var runClusterTUIFn = runClusterTUI

func runCluster(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: cluster <doctor|get|describe|logs|apply|tui>")
	}

	switch args[0] {
	case "doctor":
		return runDoctor(args[1:])
	case "get":
		return runGet(args[1:])
	case "describe":
		return runDescribe(args[1:])
	case "logs":
		return runLogs(args[1:])
	case "apply":
		return runApply(args[1:])
	case "tui":
		return runClusterTUIFn(args[1:])
	default:
		return fmt.Errorf("unknown cluster subcommand %q", args[0])
	}
}

func warnDeprecated(oldCmd, newCmd string) {
	fmt.Fprintf(os.Stderr, "warning: %s is deprecated; use %s\n", oldCmd, newCmd)
}
