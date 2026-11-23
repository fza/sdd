package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheRetiredSyncCooldownIsReadPastSilently(t *testing.T) {
	root := canonicalTemp(t)
	if err := os.MkdirAll(filepath.Join(root, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, root)
	writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/cooldown\nlanguage: en\nsync:\n  cooldown: 5m\n")
	writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\nsync:\n  cooldown: often\n")

	out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
	if err != nil {
		t.Fatalf("a retired cooldown should not stop a command: %v\n%s", err, out)
	}
	if strings.Contains(out, "sync.cooldown") {
		t.Errorf("a retired cooldown should raise no warning, got %q", out)
	}

	out, err = runAppErr(t, root, "config")
	if err != nil {
		t.Fatalf("the effective config should print: %v\n%s", err, out)
	}
	if strings.Contains(out, "sync.cooldown") {
		t.Errorf("the effective view should neither list nor warn about the retired cooldown, got %q", out)
	}

	for _, value := range []string{"30s", "often", "2m"} {
		out, err := runAppErr(t, root, "config", "set", "--local", "sync.cooldown", value)
		if err == nil || !strings.Contains(err.Error(), "no longer used") {
			t.Errorf("config set should refuse the retired sync.cooldown whatever its value, got %v\n%s", err, out)
		}
	}
}
