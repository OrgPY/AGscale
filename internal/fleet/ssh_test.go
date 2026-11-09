package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSSHConfigArgs(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	if err := os.WriteFile(cfg, []byte("Host *\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := SSHConfigArgs(cfg); len(got) != 2 || got[0] != "-F" || got[1] != cfg {
		t.Fatalf("existing config: got %v", got)
	}
	if got := SSHConfigArgs(filepath.Join(dir, "missing")); got != nil {
		t.Fatalf("missing config should yield no args, got %v", got)
	}
	if got := SSHConfigArgs(dir); got != nil {
		t.Fatalf("directory should yield no args, got %v", got)
	}
	if got := SSHConfigArgs(""); got != nil {
		t.Fatalf("empty path should yield no args, got %v", got)
	}
}

func TestValidateSSHTarget(t *testing.T) {
	for _, ok := range []string{"prodbox", "user@10.0.0.5", "host.example.com"} {
		if err := ValidateSSHTarget(ok); err != nil {
			t.Errorf("ValidateSSHTarget(%q) unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "  ", "-oProxyCommand=sh", "host -oFoo"} {
		if err := ValidateSSHTarget(bad); err == nil {
			t.Errorf("ValidateSSHTarget(%q) expected error", bad)
		}
	}
}
