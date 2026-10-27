package meta_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

func TestSyncModeIsSetPerPerson(t *testing.T) {
	auto := model.BaseConfig{Sync: model.SyncConfig{Mode: model.SyncModeAuto}}
	cases := []struct {
		name      string
		global    model.BaseConfig
		committed string
		local     string
		want      model.SyncMode
	}{
		{name: "committed auto is not honored", committed: "sync:\n  mode: auto\n", want: model.SyncModeManual},
		{name: "committed manual does not override global auto", global: auto, committed: "sync:\n  mode: manual\n", want: model.SyncModeAuto},
		{name: "local auto", local: "sync:\n  mode: auto\n", want: model.SyncModeAuto},
		{name: "global auto", global: auto, want: model.SyncModeAuto},
		{name: "local overrides global", global: auto, local: "sync:\n  mode: manual\n", want: model.SyncModeManual},
		{name: "unknown local value stays manual", local: "sync:\n  mode: always\n", want: model.SyncModeManual},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sddDir := t.TempDir()
			for name, content := range map[string]string{"config.yaml": tc.committed + "repo_id: example.com/r\n", "config.local.yaml": tc.local} {
				if err := os.WriteFile(filepath.Join(sddDir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := meta.ResolveConfig(tc.global, sddDir)
			if err != nil {
				t.Fatalf("every layer should load: %v", err)
			}
			if got := model.ResolveSyncMode(cfg); got != tc.want {
				t.Errorf("only a person's own layers should set the mode, got %q", got)
			}
		})
	}
}
