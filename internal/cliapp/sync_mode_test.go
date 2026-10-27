package cliapp_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCommittedSyncModeWarnsAndCommandsRun(t *testing.T) {
	root := canonicalTemp(t)
	if err := os.MkdirAll(filepath.Join(root, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, root)
	writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/sync\nlanguage: en\nsync:\n  mode: auto\n")
	writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\n")

	out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
	if err != nil {
		t.Fatalf("a committed mode should not stop a command: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ignoring config setting") || !strings.Contains(out, "sync.mode") {
		t.Errorf("a committed mode should be reported as ignored, got %q", out)
	}

	out, err = runAppErr(t, root, "config")
	if err != nil {
		t.Fatalf("the effective config should print: %v\n%s", err, out)
	}
	if !regexp.MustCompile(`not honored where they stand:\n\s+sync\.mode\s+\(.*config\.yaml\): only`).MatchString(out) {
		t.Errorf("the effective view should list the committed mode as not honored, got %q", out)
	}
	if !regexp.MustCompile(`(?m)^sync\.mode\s+manual\s+\(default\)`).MatchString(out) {
		t.Errorf("the effective view should show the mode as manual, untouched by the committed file, got %q", out)
	}
}
