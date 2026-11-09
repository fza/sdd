package meta_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

const hookLayer = "hooks:\n  - event: capture.preflight.left\n    command: lint\n"

func TestHooksResolveFromTheLocalLayer(t *testing.T) {
	sddDir := t.TempDir()
	writeLayer(t, filepath.Join(sddDir, "config.yaml"), "graph_dir: .sdd/graph\n")
	writeLayer(t, filepath.Join(sddDir, meta.LocalConfigFileName), hookLayer)

	cfg, err := meta.ReadConfig(sddDir, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []model.HookConfig{{Event: "capture.preflight.left", Command: "lint"}}
	if len(cfg.Hooks) != 1 || cfg.Hooks[0] != want[0] {
		t.Fatalf("hooks = %+v, want %+v", cfg.Hooks, want)
	}
}

func TestHooksInTheCommittedConfigAreRefusedNamingTheFile(t *testing.T) {
	sddDir := t.TempDir()
	committedPath := filepath.Join(sddDir, "config.yaml")
	writeLayer(t, committedPath, "graph_dir: .sdd/graph\n"+hookLayer)

	_, err := meta.ReadConfig(sddDir, "")
	if err == nil {
		t.Fatal("a committed hooks key resolved; want it refused")
	}
	for _, want := range []string{committedPath, "user-global config", meta.LocalConfigFileName} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
}

func TestHooksConcatenateGlobalThenLocal(t *testing.T) {
	sddDir := t.TempDir()
	writeLayer(t, filepath.Join(sddDir, "config.yaml"), "graph_dir: .sdd/graph\n")
	writeLayer(t, filepath.Join(sddDir, meta.LocalConfigFileName), hookLayer)
	global := model.BaseConfig{Hooks: []model.HookConfig{{Event: "entry.written", Command: "notify"}}}

	cfg, err := meta.ResolveConfig(global, sddDir, "")
	if err != nil {
		t.Fatal(err)
	}
	var commands []string
	for _, hook := range cfg.Hooks {
		commands = append(commands, hook.Command)
	}
	if strings.Join(commands, ",") != "notify,lint" {
		t.Fatalf("hook commands = %v, want the global hook then the local one", commands)
	}
}
