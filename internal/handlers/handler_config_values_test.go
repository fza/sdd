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
