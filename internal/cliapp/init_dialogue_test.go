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

func TestInitRecordsTheDialogueMode(t *testing.T) {
	t.Run("in this repository", func(t *testing.T) {
		work, _ := workRepository(t)

		runApp(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--dialogue-mode", "autonomous")

		if got := readDialogueMode(t, filepath.Join(work, ".sdd", "config.local.yaml")); got != model.DialogueAutonomous {
			t.Errorf("the dialogue mode should be recorded in the person's local config, got %q", got)
		}
		if got := readDialogueMode(t, filepath.Join(work, ".sdd", "config.yaml")); got != "" {
			t.Errorf("the dialogue mode should stay out of the committed config, got %q", got)
		}
	})
	t.Run("in the project holding the graph", func(t *testing.T) {
		work, _ := workRepository(t)

		runApp(t, work, "init", "--project-dir", "../work-sdd", "--graph-dir", "graph", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--dialogue-mode", "guided")

		project := filepath.Join(filepath.Dir(work), "work-sdd")
		if got := readDialogueMode(t, filepath.Join(project, ".sdd", "config.local.yaml")); got != model.DialogueGuided {
			t.Errorf("an explicit guided mode should be recorded where the graph lives, got %q", got)
		}
	})
}

func TestInitRefusesAnUnknownDialogueMode(t *testing.T) {
	work, _ := workRepository(t)

	_, err := runAppErr(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--dialogue-mode", "free")

	if err == nil || !strings.Contains(err.Error(), "dialogue.mode") {
		t.Errorf("an unknown dialogue mode should be refused naming the key, got %v", err)
	}
}
