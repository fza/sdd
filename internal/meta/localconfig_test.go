package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// An explicit local-config path stands in for the in-repo layer: the file
// under .sdd/ is not consulted at all, while the committed layer still
// applies underneath.
func TestReadConfig_LocalOverrideReplacesRepoLayer(t *testing.T) {
	sddDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("graph_dir: .sdd/graph\nllm:\n  provider: ollama\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sddDir, meta.LocalConfigFileName), []byte("participant: InRepo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(t.TempDir(), "elsewhere.yaml")
	if err := os.WriteFile(override, []byte("participant: FromOverride\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := meta.ReadConfig(sddDir, override)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Participant != "FromOverride" {
		t.Errorf("participant = %q, want the override file's value", cfg.Participant)
	}
	if cfg.GraphDir != ".sdd/graph" || cfg.LLM.Provider != "ollama" {
		t.Errorf("committed layer lost: %+v", cfg)
	}
}

// The provenance view reports the override as the local layer, so
// `sdd config list` describes the file the run actually read.
func TestReadConfigLayers_LocalOverrideIsTheLocalLayer(t *testing.T) {
	sddDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sddDir, meta.LocalConfigFileName), []byte("participant: InRepo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(t.TempDir(), "elsewhere.yaml")
	if err := os.WriteFile(override, []byte("participant: FromOverride\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	committed, local, err := meta.ReadConfigLayers(sddDir, override)
	if err != nil {
		t.Fatalf("ReadConfigLayers: %v", err)
	}
	if committed != nil {
		t.Errorf("no committed file present, got %+v", committed)
	}
	if local == nil || local.Participant != "FromOverride" {
		t.Errorf("local layer = %+v, want the override file's value", local)
	}
}

// A parse failure names the override path, not the in-repo one — the error
// has to point at the file the user actually passed.
func TestReadConfig_LocalOverrideErrorNamesOverridePath(t *testing.T) {
	sddDir := t.TempDir()
	override := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(override, []byte("llm:\n  concurrency: not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := meta.ReadConfig(sddDir, override)
	if err == nil {
		t.Fatal("an undecodable value must fail")
	}
	if !strings.Contains(err.Error(), override) {
		t.Errorf("error should name %s, got: %v", override, err)
	}
}

// Outside any .sdd/ directory the override still overlays the user-global
// base — the flag names a file, not a location inside a repo.
func TestResolveConfig_LocalOverrideWithoutSDDDir(t *testing.T) {
	override := filepath.Join(t.TempDir(), "standalone.yaml")
	if err := os.WriteFile(override, []byte("llm:\n  model: override-model\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := meta.ResolveConfig(model.BaseConfig{Participant: "FromGlobal"}, "", override)
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("override layer must resolve without a .sdd dir")
	}
	if cfg.LLM.Model != "override-model" {
		t.Errorf("llm.model = %q, want the override file's value", cfg.LLM.Model)
	}
	if cfg.Participant != "FromGlobal" {
		t.Errorf("untouched field should inherit from global, got %q", cfg.Participant)
	}
}

// Without .sdd/ and without an override there is no local layer to find —
// an empty directory must never resolve to a cwd-relative read.
func TestReadConfigLayers_NoSDDDirAndNoOverrideReadsNothing(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(meta.LocalConfigFileName, []byte("participant: Ambient\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	committed, local, err := meta.ReadConfigLayers("", "")
	if err != nil {
		t.Fatalf("ReadConfigLayers: %v", err)
	}
	if committed != nil || local != nil {
		t.Errorf("no layers expected, got committed=%+v local=%+v", committed, local)
	}
}
