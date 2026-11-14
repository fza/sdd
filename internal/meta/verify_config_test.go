package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

func TestResolveConfig_RefusesAnUnknownVerifyMode(t *testing.T) {
	sddDir := t.TempDir()
	err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("verify:\n  mode: light\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = meta.ResolveConfig(model.BaseConfig{}, sddDir, "")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(sddDir, "config.yaml")+`: verify.mode "light"`) {
		t.Fatalf("a mode outside the set should be refused naming the file and key, got %v", err)
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
