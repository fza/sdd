package meta_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
)

func TestEventSharingIsReadOnlyFromTheCommittedConfig(t *testing.T) {
	sddDir := filepath.Join(t.TempDir(), ".sdd")
	err := os.MkdirAll(sddDir, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("events:\n  channels:\n    wip:\n      share: git\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("events:\n  channels:\n    recheck:\n      share: git\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := meta.ReadConfig(sddDir, "")
	if err != nil {
		t.Fatal(err)
	}
	channels, err := cfg.Events.GitChannels()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(channels, []string{"recheck"}) {
		t.Errorf("git channels = %v; the committed share should hold and the local one be ignored", channels)
	}
}
