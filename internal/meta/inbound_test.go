package meta_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
)

func TestInboundIsReadOnlyFromTheCommittedConfig(t *testing.T) {
	sddDir := filepath.Join(t.TempDir(), ".sdd")
	err := os.MkdirAll(sddDir, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("inbound:\n  accept: true\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := meta.ReadConfig(sddDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Inbound.Accepts("github.com/acme/app") {
		t.Error("inbound set in config.local.yaml should not be honored")
	}

	err = os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("inbound:\n  accept: true\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = meta.ReadConfig(sddDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Inbound.Accepts("github.com/acme/app") {
		t.Error("inbound in the committed config should be honored")
	}
}
