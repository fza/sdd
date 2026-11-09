package meta_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

func hookLayer(command string) string {
	return "hooks:\n  - event: capture.assemble\n    command: " + command + "\n    args: [--config, vale.ini]\n    files: [styles/*]\n"
}

// A project ships hooks in its committed config (20261109-213250-d-tac-bve).
func TestHooksResolveFromTheCommittedLayer(t *testing.T) {
	sddDir := t.TempDir()
	writeLayer(t, filepath.Join(sddDir, "config.yaml"), "graph_dir: .sdd/graph\n"+hookLayer("vale.sh"))

	cfg, err := meta.ReadConfig(sddDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hooks) != 1 || cfg.Hooks[0].Command != "vale.sh" || strings.Join(cfg.Hooks[0].Args, " ") != "--config vale.ini" || strings.Join(cfg.Hooks[0].Files, " ") != "styles/*" {
		t.Fatalf("hooks = %+v, want the committed hook with its args and files", cfg.Hooks)
	}
}

func TestHooksConcatenateGlobalThenCommittedThenLocal(t *testing.T) {
	sddDir := t.TempDir()
	writeLayer(t, filepath.Join(sddDir, "config.yaml"), "graph_dir: .sdd/graph\n"+hookLayer("committed.sh"))
	writeLayer(t, filepath.Join(sddDir, meta.LocalConfigFileName), hookLayer("local/mine.sh"))
	global := model.BaseConfig{Hooks: []model.HookConfig{{Event: "entry.written", Command: "notify"}}}

	cfg, err := meta.ResolveConfig(global, sddDir, "")
	if err != nil {
		t.Fatal(err)
	}
	var commands []string
	for _, hook := range cfg.Hooks {
		commands = append(commands, hook.Command)
	}
	if strings.Join(commands, ",") != "notify,committed.sh,local/mine.sh" {
		t.Fatalf("hook commands = %v, want the global, the committed, then the local hook", commands)
	}
}
