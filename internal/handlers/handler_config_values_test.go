package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
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
// naming the allowed values (20261110-015917-d-tac-yc6).
func TestConfigSetRefusesAValueOutsideTheAllowedSet(t *testing.T) {
	cases := []struct {
		target, key, bogus, allowed, valid string
	}{
		{"global", "verify.mode", "light", "off, grounding, prose or deep", "deep"},
		{"local", "verify.mode", "light", "off, grounding, prose or deep", "deep"},
		{"global", "verify.skip", "sometimes", "weighed, failure or never", "never"},
		{"global", "verify.runner", "docker", "subagent, claude, codex or driver", "codex"},
		{"local", "verify.runner", "docker", "subagent, claude, codex or driver", "claude"},
		{"global", "verify.claude.effort", "ultra", "low, medium, high, xhigh or max", "xhigh"},
		{"local", "dialogue.mode", "free", "guided or autonomous", "autonomous"},
		{"global", "llm.structured_output", "xml", "json_schema, json_object or auto", "json_object"},
		{"global", "llm_prefilter.protocol", "bm25", "systemone, cohere-rerank or generate", "generate"},
		{"global", "llm_prefilter.structured_output", "xml", "json_schema, json_object or auto", "auto"},
		{"local", "embedding.provider", "voyage", "openai or ollama", "ollama"},
		{"local", "skill_scope", "machine", "user or project", "project"},
		{"local", "events.channels.recheck.share", "s3", "local or git", "git"},
		{"local", "relevance.content", "none", "all or reader", "reader"},
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

// The cross-layer verify check loading runs judges the files as they stand
// after the write, for the key being set alone.
func TestConfigSetRunsTheVerifyLayerCheckOnTheWrittenFiles(t *testing.T) {
	cases := []struct {
		name    string
		seed    configLayers
		target  string
		key     string
		value   string
		refusal string
	}{
		{
			name:    "local mode below the committed level",
			seed:    configLayers{committed: "verify:\n  mode: deep\n"},
			target:  "local",
			key:     "verify.mode",
			value:   "prose",
			refusal: "verify.mode prose is refused in the machine-local config",
		},
		{
			name:    "local mode below the default level",
			target:  "local",
			key:     "verify.mode",
			value:   "grounding",
			refusal: "verify.mode grounding is refused in the machine-local config",
		},
		{
			name:    "global mode raised above the local one",
			seed:    configLayers{local: "verify:\n  mode: prose\n"},
			target:  "global",
			key:     "verify.mode",
			value:   "deep",
			refusal: "verify.mode prose is refused in the machine-local config",
		},
		{
			name:    "local skip looser than the committed one",
			seed:    configLayers{committed: "verify:\n  skip: never\n"},
			target:  "local",
			key:     "verify.skip",
			value:   "failure",
			refusal: "verify.skip failure is refused in the machine-local config",
		},
		{
			name:    "driver runner without a command",
			target:  "local",
			key:     "verify.runner",
			value:   "driver",
			refusal: "verify.runner driver needs verify.driver.command",
		},
		{
			name:   "local mode raising the committed level",
			seed:   configLayers{committed: "verify:\n  mode: prose\n"},
			target: "local",
			key:    "verify.mode",
			value:  "deep",
		},
		{
			name:   "a refusal under another verify key does not block the write",
			seed:   configLayers{committed: "verify:\n  skip: never\n", local: "verify:\n  skip: weighed\n"},
			target: "local",
			key:    "verify.mode",
			value:  "deep",
		},
		{
			name:   "an unknown key in the file does not block the write",
			seed:   configLayers{local: "telemetry:\n  endpoint: https://example.invalid\nverify:\n  future: x\n"},
			target: "local",
			key:    "verify.mode",
			value:  "deep",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, paths := newConfigValuesHandler(t, tc.seed)
			path := paths.global
			if tc.target == "local" {
				path = paths.local
			}
			before := readOrEmpty(t, path)

			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: tc.key, Value: tc.value})
			if tc.refusal == "" {
				if err != nil {
					t.Fatalf("the write should land: %v", err)
				}
				if !strings.Contains(readOrEmpty(t, path), "mode: "+tc.value) {
					t.Error("the value should land in the file")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("the write should be refused with the message loading gives, got %v", err)
			}
			if readOrEmpty(t, path) != before {
				t.Error("a refused write should leave the file untouched")
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
		{"global", "verify.mode"},
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
	err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "dialogue.mode", Value: "guided"})
	if err != nil {
		t.Fatalf("an unknown key elsewhere should not block the write: %v", err)
	}
	written := readOrEmpty(t, paths.global)
	if !strings.Contains(written, "mode: guided") || !strings.Contains(written, "telemetry:") {
		t.Errorf("the value should land beside the unknown key, got %s", written)
	}
}
