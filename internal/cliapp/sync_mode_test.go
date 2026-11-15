package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredSyncKeysAreReadPastSilently(t *testing.T) {
	root := canonicalTemp(t)
	if err := os.MkdirAll(filepath.Join(root, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, root)
	writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/sync\nlanguage: en\nsync:\n  mode: auto\n  cooldown: 5m\n")
	writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\nsync:\n  mode: always\n")

	out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
	if err != nil {
		t.Fatalf("a retired sync key should not stop a command: %v\n%s", err, out)
	}
	if strings.Contains(out, "sync.mode") {
		t.Errorf("a retired sync key should raise no warning, got %q", out)
	}

	out, err = runAppErr(t, root, "config")
	if err != nil {
		t.Fatalf("the effective config should print: %v\n%s", err, out)
	}
	for _, key := range []string{"sync.mode", "sync.cooldown"} {
		if strings.Contains(out, key) {
			t.Errorf("the effective view should neither list nor warn about a retired sync key, got %q", out)
		}
	}
}
