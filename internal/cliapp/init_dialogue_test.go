package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func readDialogueMode(t *testing.T, path string) model.DialogueMode {
	t.Helper()
	cfg, err := model.ParseConfig([]byte(readFile(t, path)))
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if cfg == nil {
		return ""
	}
	return cfg.Dialogue.Mode
}

func initFixture(t *testing.T) *appFixture {
	t.Helper()
	f := newAppFixture(t)
	runGit(t, f.root, "init", "-q", "-b", "main")
	runGit(t, f.root, "config", "user.email", "test@example.com")
	runGit(t, f.root, "config", "user.name", "Test")
	runGit(t, f.root, "add", ".")
	runGit(t, f.root, "commit", "-q", "-m", "chore: seed")
	return f
}

func TestInitRecordsTheDialogueMode(t *testing.T) {
	f := initFixture(t)

	result := f.run(t, "init", "--scope", "project", "--agents", "claude", "--participant", "Ada", "--dialogue-mode", "auto")
	if result.err != nil {
		t.Fatalf("init: %v\n%s", result.err, result.stderr)
	}

	if got := readDialogueMode(t, filepath.Join(f.root, ".sdd", "config.local.yaml")); got != model.DialogueAuto {
		t.Errorf("the dialogue mode should be recorded in the person's local config, got %q", got)
	}
	if got := readDialogueMode(t, filepath.Join(f.root, ".sdd", "config.yaml")); got != "" {
		t.Errorf("the dialogue mode should stay out of the committed config, got %q", got)
	}
}

func TestInitRefusesAnUnknownDialogueMode(t *testing.T) {
	f := initFixture(t)

	result := f.run(t, "init", "--scope", "project", "--agents", "claude", "--participant", "Ada", "--dialogue-mode", "free")

	if result.err == nil || !strings.Contains(result.err.Error(), "dialogue.mode") {
		t.Errorf("an unknown dialogue mode should be refused naming the key, got %v", result.err)
	}
}
