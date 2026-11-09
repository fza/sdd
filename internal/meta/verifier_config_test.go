package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// A verifier mode without grounding enforcement is refused when the config
// resolves, naming both keys and the files that set them (V-D7).
func TestResolveConfig_RefusesAVerifierWithoutGrounding(t *testing.T) {
	sddDir := t.TempDir()
	err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("verifier:\n  mode: light\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = meta.ResolveConfig(model.BaseConfig{}, sddDir, "")
	if err == nil {
		t.Fatal("light without grounding enforcement should be refused")
	}
	for _, want := range []string{"verifier.mode light", filepath.Join(sddDir, "config.yaml"), "grounding.enforce"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal should name %q: %v", want, err)
		}
	}

	err = os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("grounding:\n  enforce: true\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := meta.ResolveConfig(model.BaseConfig{}, sddDir, "")
	if err != nil {
		t.Fatalf("a local layer raising enforcement makes the pair hold: %v", err)
	}
	if cfg.Verifier.Policy().Mode != model.VerifierLight {
		t.Fatalf("the committed mode should resolve, got %s", cfg.Verifier.Policy().Mode)
	}
}

func TestResolveConfig_RefusesALocalLoweringTheVerifier(t *testing.T) {
	sddDir := t.TempDir()
	err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("grounding:\n  enforce: true\nverifier:\n  mode: deep\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("verifier:\n  mode: off\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = meta.ResolveConfig(model.BaseConfig{}, sddDir, "")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(sddDir, "config.local.yaml")) || !strings.Contains(err.Error(), "verifier.mode off") {
		t.Fatalf("a local layer lowering the mode should be refused naming the file and key, got %v", err)
	}
}
