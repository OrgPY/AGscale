package fleet

import (
	"fmt"
	"os"
	"strings"
)

// SSHConfigArgs returns the "-F <path>" arguments for ssh only when path names
// an existing regular file. ssh exits 255 when -F points at a missing file, so
// passing the default ~/.ssh/config unconditionally breaks hosts without one.
func SSHConfigArgs(path string) []string {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil
	}
	if info, err := os.Stat(p); err != nil || info.IsDir() {
		return nil
	}
	return []string{"-F", p}
}

// ValidateSSHTarget rejects targets that ssh would interpret as command-line
// options (e.g. "-oProxyCommand=..."), which would otherwise allow argument
// injection via host names stored in fleet state files.
func ValidateSSHTarget(target string) error {
	t := strings.TrimSpace(target)
	if t == "" {
		return fmt.Errorf("empty ssh target")
	}
	if strings.HasPrefix(t, "-") {
		return fmt.Errorf("invalid ssh target %q: must not start with '-'", t)
	}
	if strings.ContainsAny(t, " \t\r\n") {
		return fmt.Errorf("invalid ssh target %q: must not contain whitespace", t)
	}
	return nil
}
