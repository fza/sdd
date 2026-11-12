package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

func TestResolveConfig_AnUnknownVerifyModeLeavesTheLayerBelow(t *testing.T) {
	sddDir := t.TempDir()
	err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("verify:\n  mode: light\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	var global model.BaseConfig
	global.Verify.Mode = model.VerifyDeep
	for _, tc := range []struct {
		name   string
		global model.BaseConfig
		want   model.VerifyMode
	}{
		{"under a global mode", global, model.VerifyDeep},
		{"alone", model.BaseConfig{}, model.DefaultVerifyMode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := meta.ResolveConfig(tc.global, sddDir, "")
			if err != nil {
				t.Fatalf("a mode outside the set should not fail the read: %v", err)
			}
			if got := cfg.Verify.ResolvedMode(); got != tc.want {
				t.Fatalf("the unknown mode should leave the layer below, got %q", got)
			}
		})
	}
}

func TestResolveConfig_RefusesALocalLoweringTheVerifier(t *testing.T) {
	sddDir := t.TempDir()
	err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("verify:\n  mode: deep\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("verify:\n  mode: prose\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = meta.ResolveConfig(model.BaseConfig{}, sddDir, "")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(sddDir, "config.local.yaml")) || !strings.Contains(err.Error(), "verify.mode prose") {
		t.Fatalf("a local layer lowering the mode should be refused naming the file and key, got %v", err)
	}
}
