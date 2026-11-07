package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// ResolveConfig composes the full overlay: global base under the committed
// config.yaml under config.local.yaml — later layers win, untouched fields
// inherit.
func TestResolveConfig_OverlayOrder(t *testing.T) {
	sddDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("participant: FromCommitted\ngraph_dir: .sdd/graph\nllm:\n  model: committed-model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("llm:\n  model: local-model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	global := model.BaseConfig{
		Participant: "FromGlobal",
		LLM:         model.LLMConfig{Provider: "ollama", Model: "global-model"},
	}
	cfg, err := ResolveConfig(global, sddDir, "")
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	if cfg.Participant != "FromCommitted" {
		t.Errorf("committed file should override global participant, got %q", cfg.Participant)
	}
	if cfg.LLM.Model != "local-model" {
		t.Errorf("local file should win over committed and global, got %q", cfg.LLM.Model)
	}
	if cfg.LLM.Provider != "ollama" {
		t.Errorf("untouched field should inherit from global, got %q", cfg.LLM.Provider)
	}
	if cfg.GraphDir != ".sdd/graph" {
		t.Errorf("per-repo field lost: %q", cfg.GraphDir)
	}
}

// With no per-repo config files, global settings still resolve; with no
// layers at all, ResolveConfig keeps the legacy nil "no config" contract.
func TestResolveConfig_GlobalOnlyAndEmpty(t *testing.T) {
	sddDir := t.TempDir()
	cfg, err := ResolveConfig(model.BaseConfig{Participant: "Christopher"}, sddDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.Participant != "Christopher" {
		t.Errorf("global-only resolution failed: %+v", cfg)
	}
	cfg, err = ResolveConfig(model.BaseConfig{}, sddDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Errorf("no layers should resolve to nil, got %+v", cfg)
	}
}

// A parse failure names the offending file — the error carries the path of
// the exact layer that broke.
func TestReadConfig_ErrorNamesFile(t *testing.T) {
	sddDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("llm:\n  concurrency: not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadConfig(sddDir, "")
	if err == nil {
		t.Fatal("an undecodable value must fail")
	}
	if !strings.Contains(err.Error(), "config.local.yaml") {
		t.Errorf("error should name the file, got: %v", err)
	}
}

// A key this layer does not declare is read past, and the keys it does know
// still decode.
func TestReadConfig_ToleratesForeignKey(t *testing.T) {
	sddDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("participant: Christopher\nrepos:\n  - repo_id: a/b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadConfig(sddDir, "")
	if err != nil {
		t.Fatalf("a foreign key must not stop the load: %v", err)
	}
	if cfg == nil || cfg.Participant != "Christopher" {
		t.Errorf("the keys it does know must still decode: %+v", cfg)
	}
}
