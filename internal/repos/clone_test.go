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

func writeCloneConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".sdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sdd", "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCloneReadsCommittedConfig(t *testing.T) {
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
			clone := repos.Clone{RepoID: repoID, Dir: t.TempDir()}
			if tt.config != "" {
				writeCloneConfig(t, clone.Dir, tt.config)
			}

			_, err := clone.Config()
			if got := errors.Is(err, meta.ErrNotAnSDDProject); got != tt.notSDD {
				t.Errorf("Config error %v: not-an-SDD-project = %v, want %v", err, got, tt.notSDD)
			}
			graphDir, err := clone.GraphDir()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GraphDir error = %v, want one naming %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(clone.Dir, filepath.FromSlash(tt.wantGraphDir)); graphDir != want {
				t.Errorf("GraphDir = %q, want %q", graphDir, want)
			}
		})
	}
}

func TestRegistryCloneOnlyForConnectedRepos(t *testing.T) {
	loc := repos.Locations{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), CacheRoot: t.TempDir(), DataRoot: t.TempDir()}
	if err := os.WriteFile(loc.ConfigPath, []byte("repos:\n  - repo_id: example.test/dep\n    clone_url: https://example.test/dep.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := repos.NewRegistry(loc)

	clone, ok, err := reg.Clone("example.test/dep")
	if err != nil || !ok {
		t.Fatalf("connected repo: ok=%v err=%v", ok, err)
	}
	if want := filepath.Join(loc.DataRoot, "repos", "example.test", "dep"); clone.Dir != want || clone.RepoID != "example.test/dep" {
		t.Errorf("Clone = %+v, want dir %q", clone, want)
	}
	if clone.Cloned() {
		t.Error("an empty clone dir reports cloned")
	}

	if _, ok, err := reg.Clone("example.test/other"); err != nil || ok {
		t.Errorf("unconnected repo: ok=%v err=%v, want not ok", ok, err)
	}
}
