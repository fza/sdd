package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/repos"
)

type configLayers struct {
	global, committed, local string
}

func newConfigValuesHandler(t *testing.T, seed configLayers) (*handlers.Handler, configLayers) {
	t.Helper()
	dir := t.TempDir()
	sddDir := filepath.Join(dir, ".sdd")
	if err := os.MkdirAll(sddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := configLayers{
		global:    filepath.Join(dir, "global.yaml"),
		committed: filepath.Join(sddDir, "config.yaml"),
		local:     filepath.Join(sddDir, "config.local.yaml"),
	}
	for path, content := range map[string]string{paths.global: seed.global, paths.committed: seed.committed, paths.local: seed.local} {
		if content == "" {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reg := repos.NewRegistry(repos.Locations{ConfigPath: paths.global, CacheRoot: filepath.Join(dir, "cache")})
	return handlers.New(handlers.Options{SDDDir: sddDir, Repos: repos.NewManager(reg, nil)}), paths
}

func readOrEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A value outside the set its setting allows is refused before it lands,
// naming the allowed values.
func TestConfigSetRefusesAValueOutsideTheAllowedSet(t *testing.T) {
	cases := []struct {
		target, key, bogus, allowed, valid string
	}{
		{"global", "embedding.provider", "voyage", "openai or ollama", "openai"},
		{"local", "embedding.provider", "voyage", "openai or ollama", "ollama"},
		{"local", "skill_scope", "machine", "user or project", "project"},
		{"local", "llm.provider", "voyage", "claude-cli, anthropic, openai or ollama", "ollama"},
	}
	for _, tc := range cases {
		t.Run(tc.target+" "+tc.key, func(t *testing.T) {
			h, paths := newConfigValuesHandler(t, configLayers{global: "participant: Ada\n", local: "participant: Ada\n"})
			path := paths.global
			if tc.target == "local" {
				path = paths.local
			}
			before := readOrEmpty(t, path)

			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: tc.key, Value: tc.bogus})
			if err == nil || !strings.Contains(err.Error(), tc.key) || !strings.Contains(err.Error(), tc.allowed) {
				t.Fatalf("the value should be refused naming the key and the allowed values, got %v", err)
			}
			if readOrEmpty(t, path) != before {
				t.Fatal("a refused value should leave the file untouched")
			}

			err = h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: tc.key, Value: tc.valid})
			if err != nil {
				t.Fatalf("an allowed value should be written: %v", err)
			}
			if !strings.Contains(readOrEmpty(t, path), tc.valid) {
				t.Error("the allowed value should land in the file")
			}
		})
	}
}

// `sdd config unset` removes a key, so an empty value is no setting.
func TestConfigSetRefusesAnEmptyValue(t *testing.T) {
	for _, tc := range []struct{ target, key string }{
		{"global", "participant"},
		{"local", "participant"},
		{"local", "llm.model"},
		{"global", "llm.model"},
	} {
		t.Run(tc.target+" "+tc.key, func(t *testing.T) {
			h, paths := newConfigValuesHandler(t, configLayers{})
			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: tc.key, Value: ""})
			if err == nil || !strings.Contains(err.Error(), "takes no empty value") {
				t.Fatalf("an empty value should be refused, got %v", err)
			}
			if readOrEmpty(t, paths.global) != "" || readOrEmpty(t, paths.local) != "" {
				t.Error("a refused value should write no file")
			}
		})
	}
}

// A key some other sdd version wrote never blocks a write to a known key.
func TestConfigSetWritesBesideAnUnknownKey(t *testing.T) {
	h, paths := newConfigValuesHandler(t, configLayers{global: "telemetry:\n  endpoint: https://example.invalid\n"})
	err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "embedding.provider", Value: "ollama"})
	if err != nil {
		t.Fatalf("an unknown key elsewhere should not block the write: %v", err)
	}
	written := readOrEmpty(t, paths.global)
	if !strings.Contains(written, "provider: ollama") || !strings.Contains(written, "telemetry:") {
		t.Errorf("the value should land beside the unknown key, got %s", written)
	}
}

func TestConfigSetJudgesThePrefilterProvider(t *testing.T) {
	cases := []struct {
		target, value string
		refused       bool
	}{
		{target: "global", value: "systemone"},
		{target: "local", value: "cohere"},
		{target: "global", value: "openai"},
		{target: "global", value: "bm25", refused: true},
		{target: "local", value: "opper", refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.target+" "+tc.value, func(t *testing.T) {
			dir := t.TempDir()
			sddDir := filepath.Join(dir, ".sdd")
			err := os.MkdirAll(sddDir, 0o755)
			if err != nil {
				t.Fatal(err)
			}
			globalPath := filepath.Join(dir, "global.yaml")
			reg := repos.NewRegistry(repos.Locations{ConfigPath: globalPath, CacheRoot: filepath.Join(dir, "cache")})
			h := handlers.New(handlers.Options{SDDDir: sddDir, Repos: repos.NewManager(reg, git.CLI{})})
			path := globalPath
			if tc.target == "local" {
				path = filepath.Join(sddDir, "config.local.yaml")
			}

			err = h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: "llm_prefilter.provider", Value: tc.value})
			written, _ := os.ReadFile(path)
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "llm_prefilter.provider") || !strings.Contains(err.Error(), "systemone or cohere") {
					t.Errorf("a provider outside the set should be refused naming the key and the allowed values, got %v", err)
				}
				if strings.Contains(string(written), tc.value) {
					t.Error("a refused provider should not land in the file")
				}
				return
			}
			if err != nil {
				t.Fatalf("a provider in the set should be written: %v", err)
			}
			if !strings.Contains(string(written), tc.value) {
				t.Error("the provider should land in the file")
			}
		})
	}
}

// A dedicated judge set as llm.provider would stop the project from loading
// at its next build; config set refuses it, pointing at llm_prefilter.provider.
func TestConfigSetRefusesAPrefilterJudgeAsTheLLMProvider(t *testing.T) {
	cases := []struct {
		value   string
		refused bool
	}{
		{value: "systemone", refused: true},
		{value: "cohere", refused: true},
		{value: "openai"},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			dir := t.TempDir()
			globalPath := filepath.Join(dir, "global.yaml")
			reg := repos.NewRegistry(repos.Locations{ConfigPath: globalPath, CacheRoot: filepath.Join(dir, "cache")})
			h := handlers.New(handlers.Options{Repos: repos.NewManager(reg, git.CLI{})})

			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "llm.provider", Value: tc.value})
			written, _ := os.ReadFile(globalPath)
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "llm_prefilter.provider") || strings.Contains(string(written), tc.value) {
					t.Errorf("a prefilter judge as llm.provider should be refused, pointing at llm_prefilter.provider, got %v", err)
				}
				return
			}
			if err != nil || !strings.Contains(string(written), tc.value) {
				t.Errorf("a chat provider should be written, got %v", err)
			}
		})
	}
}
