package repos

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestLoadConfigFrom_DecodesAFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatalf("missing file must load empty: %v", err)
	}
	if len(cfg.Repos) != 0 {
		t.Fatalf("empty config has repos: %+v", cfg.Repos)
	}

	const doc = `embedding:
  provider: ollama
  model: nomic-embed-text
repos:
  - repo_id: github.com/networkteam/other
    clone_url: git@github.com:networkteam/other.git
`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	repo, ok := loaded.Connected("github.com/networkteam/other")
	if !ok || repo.CloneURL != "git@github.com:networkteam/other.git" {
		t.Errorf("connection not decoded: %+v", loaded.Repos)
	}
	if loaded.Embedding.Provider != "ollama" {
		t.Errorf("embedding config not decoded: %+v", loaded.Embedding)
	}
}

func TestAddRepo_RejectsDuplicateAndMalformedIDs(t *testing.T) {
	cfg := &GlobalConfig{}
	if err := cfg.AddRepo(ConnectedRepo{RepoID: "github.com/networkteam/other", CloneURL: "git@github.com:networkteam/other.git"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddRepo(ConnectedRepo{RepoID: "github.com/networkteam/other", CloneURL: "x"}); err == nil {
		t.Error("duplicate repo_id must be rejected")
	}
	if err := cfg.AddRepo(ConnectedRepo{RepoID: "nohost", CloneURL: "x"}); err == nil {
		t.Error("invalid repo_id must be rejected")
	}
}

// TestDefaultLocationsHonorXDG covers the one place the package touches the
// environment: the default-locations constructor composition roots call.
// Everything else works against an explicit Locations value.
func TestDefaultLocationsHonorXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")

	loc, err := DefaultLocations()
	if err != nil {
		t.Fatal(err)
	}
	if loc.ConfigPath != "/tmp/xdg-config/sdd/config.yaml" {
		t.Errorf("ConfigPath = %q", loc.ConfigPath)
	}
	if loc.CacheRoot != "/tmp/xdg-cache/sdd" {
		t.Errorf("CacheRoot = %q", loc.CacheRoot)
	}
	if loc.StateRoot != "/tmp/xdg-state/sdd" {
		t.Errorf("StateRoot = %q", loc.StateRoot)
	}

	reg := NewRegistry(loc)
	dir, err := reg.CloneDir("github.com/networkteam/other")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join("/tmp/xdg-data/sdd", "repos", "github.com", "networkteam", "other") {
		t.Errorf("CloneDir = %q", dir)
	}
	if _, err := reg.CloneDir("not a repo id"); err == nil {
		t.Error("CloneDir must validate the repo id")
	}
}

func TestDefaultLocationsRejectsRelativeStateHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	t.Setenv("XDG_STATE_HOME", "relative/state")
	if _, err := DefaultLocations(); err == nil {
		t.Fatal("relative XDG_STATE_HOME accepted")
	}
}

// The user-global config carries the shared BaseConfig settings inline —
// participant, llm, embedding, sync — next to its own repos list.
func TestGlobalConfigBaseSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "participant: Christopher\nllm:\n  provider: ollama\n  model: llama3.1:70b\nembedding:\n  provider: ollama\nrepos:\n  - repo_id: github.com/networkteam/other\n    clone_url: git@github.com:networkteam/other.git\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatalf("LoadConfigFrom: %v", err)
	}
	if cfg.Participant != "Christopher" {
		t.Errorf("Participant = %q, want Christopher", cfg.Participant)
	}
	if cfg.LLM.Provider != "ollama" || cfg.LLM.Model != "llama3.1:70b" {
		t.Errorf("LLM not decoded: %+v", cfg.LLM)
	}
	if cfg.Embedding.Provider != "ollama" {
		t.Errorf("Embedding not decoded: %+v", cfg.Embedding)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].RepoID != "github.com/networkteam/other" {
		t.Errorf("Repos not decoded: %+v", cfg.Repos)
	}
}

// The global layer loads past keys it does not declare — a per-repo-only
// setting misplaced here, or anything a newer binary wrote — and names them
// instead (20260810-144515-s-tac-8ae).
func TestGlobalConfigToleratesAndReportsForeignKeys(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		key  string
	}{
		{"per-repo-only key", "repo_id: github.com/org/repo\n", "repo_id"},
		{"per-repo graph_dir", "graph_dir: .sdd/graph\n", "graph_dir"},
		{"arbitrary unknown", "bogus: 1\n", "bogus"},
		{"nested unknown", "llm:\n  bogus_nested: 1\n", "llm.bogus_nested"},
		{"block from a newer binary", "sessions:\n  retention: 14d\ntelemetry:\n  endpoint: https://x\n", "telemetry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfigFrom(path); err != nil {
				t.Fatalf("key %q must not stop the load: %v", tc.key, err)
			}
			unknown, err := model.UnknownYAMLKeys([]byte(tc.yaml), reflect.TypeFor[GlobalConfig]())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(unknown, tc.key) {
				t.Errorf("unknown keys %v should name %q", unknown, tc.key)
			}
		})
	}
}

func TestUnconnectedDependencies(t *testing.T) {
	cfg := &GlobalConfig{Repos: []ConnectedRepo{{RepoID: "github.com/org/connected", CloneURL: "x"}}}
	missing := cfg.UnconnectedDependencies([]model.RepoID{"github.com/org/connected", "github.com/org/missing"})
	if len(missing) != 1 || missing[0] != "github.com/org/missing" {
		t.Errorf("UnconnectedDependencies = %v", missing)
	}
	if got := cfg.UnconnectedDependencies(nil); got != nil {
		t.Errorf("no deps should yield nil, got %v", got)
	}
}
