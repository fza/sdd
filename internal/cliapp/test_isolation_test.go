package cliapp_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A test that sets none or only some of the XDG base directories still works
// in temporary ones, and the sdd on PATH is the failing stand-in, so no test
// reaches the machine's clones or runs the installed sdd.
func TestTestsRunApartFromTheMachine(t *testing.T) {
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		dir := os.Getenv(name)
		if dir == "" || !strings.HasPrefix(dir, filepath.Clean(os.TempDir())) {
			t.Errorf("%s should point into the temporary directory, got %q", name, dir)
		}
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	sdd, err := exec.LookPath("sdd")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(sdd) != filepath.Dir(claude) {
		t.Errorf("the sdd on PATH should be the stand-in beside claude's, got %s", sdd)
	}
	for _, key := range []string{"commit.gpgsign", "tag.gpgsign", "maintenance.auto"} {
		out, err := exec.Command("git", "config", "--type=bool", "--get", key).Output()
		if err != nil || strings.TrimSpace(string(out)) != "false" {
			t.Errorf("git should run with %s off whatever the developer's config, got %q, %v", key, out, err)
		}
	}
}
