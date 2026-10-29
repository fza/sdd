package repos_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func writeCacheConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".sdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sdd", "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCacheReadsCommittedConfig(t *testing.T) {
	const repoID = model.RepoID("example.test/dep")
	tests := []struct {
		name         string
		config       string
		wantGraphDir string
		wantErr      string
		notSDD       bool
	}{
		{name: "declared layout", config: "repo_id: example.test/dep\ngraph_dir: decisions\ndependencies: [example.test/deep]\n", wantGraphDir: "decisions"},
		{name: "default layout", config: "repo_id: example.test/dep\n", wantGraphDir: ".sdd/graph"},
		{name: "no config", wantGraphDir: ".sdd/graph", notSDD: true},
		{name: "malformed config", config: "repo_id: [\n", wantErr: string(repoID)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := repos.Cache{RepoID: repoID, Dir: t.TempDir()}
			if tt.config != "" {
				writeCacheConfig(t, cache.Dir, tt.config)
			}

			_, err := cache.Config()
			if got := errors.Is(err, meta.ErrNotAnSDDProject); got != tt.notSDD {
				t.Errorf("Config error %v: not-an-SDD-project = %v, want %v", err, got, tt.notSDD)
			}
			graphDir, err := cache.GraphDir()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GraphDir error = %v, want one naming %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(cache.Dir, filepath.FromSlash(tt.wantGraphDir)); graphDir != want {
				t.Errorf("GraphDir = %q, want %q", graphDir, want)
			}
		})
	}
}

func TestRegistryCacheOnlyForConnectedRepos(t *testing.T) {
	loc := repos.Locations{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), CacheRoot: t.TempDir()}
	if err := os.WriteFile(loc.ConfigPath, []byte("repos:\n  - repo_id: example.test/dep\n    clone_url: https://example.test/dep.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := repos.NewRegistry(loc)

	cache, ok, err := reg.Cache("example.test/dep")
	if err != nil || !ok {
		t.Fatalf("connected repo: ok=%v err=%v", ok, err)
	}
	if want := filepath.Join(loc.CacheRoot, "example.test", "dep"); cache.Dir != want || cache.RepoID != "example.test/dep" {
		t.Errorf("Cache = %+v, want dir %q", cache, want)
	}
	if cache.Cloned() {
		t.Error("an empty cache dir reports cloned")
	}

	if _, ok, err := reg.Cache("example.test/other"); err != nil || ok {
		t.Errorf("unconnected repo: ok=%v err=%v, want not ok", ok, err)
	}
}
