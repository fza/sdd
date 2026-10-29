package finders_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/repos/repostest"
)

// TestIndexLintReportsDriftAcrossTheDependencyClosure: dep, deep (declared by
// dep) and stray (declared by nobody) are connected and cached, each with an
// index built under an older embedder. Lint reports drift for the closure
// members only, the repos a cross-repo search reaches (20261026-194528-d-cpt-gcq).
func TestIndexLintReportsDriftAcrossTheDependencyClosure(t *testing.T) {
	loc := repos.Locations{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), CacheRoot: t.TempDir()}
	reg := repos.NewRegistry(loc)
	embedding := model.EmbeddingConfig{Provider: "ollama", Model: "current"}
	connected := &repos.GlobalConfig{BaseConfig: model.BaseConfig{Embedding: embedding}}
	for repoID, config := range map[model.RepoID]string{
		"example.test/dep":   "repo_id: example.test/dep\ndependencies: [example.test/deep]\n",
		"example.test/deep":  "repo_id: example.test/deep\n",
		"example.test/stray": "repo_id: example.test/stray\n",
	} {
		if err := connected.AddRepo(repos.ConnectedRepo{RepoID: repoID, CloneURL: "https://" + string(repoID)}); err != nil {
			t.Fatal(err)
		}
		dir, err := reg.CacheDir(repoID)
		if err != nil {
			t.Fatal(err)
		}
		for _, sub := range []string{".git", ".sdd"} {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, ".sdd", "config.yaml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
		drifted := index.Manifest{Version: 1, Entries: map[string]index.EntryState{
			"20260101-100000-s-tac-aaa": {Versions: []index.EntryVersion{{Hash: "h", Fingerprint: "ollama/older"}}},
		}}
		if err := drifted.Save(index.StoreDir(loc.CacheRoot, index.RepoKey(repoID, dir), "ollama/current")); err != nil {
			t.Fatal(err)
		}
	}
	repostest.WriteConfig(t, loc.ConfigPath, connected)

	f := finders.New(finders.Options{Config: &model.PerRepoConfig{RepoID: "example.test/local", Dependencies: []model.RepoID{"example.test/dep"}}, Repos: reg})
	result := &query.LintResult{}
	f.IndexLint(query.IndexLintQuery{Embedding: embedding, IndexDir: t.TempDir()}, result)

	var drifted []string
	for _, finding := range result.Findings {
		if finding.Code == "repo-fingerprint-drift" {
			drifted = append(drifted, finding.EntryID)
		}
	}
	if want := []string{"example.test/dep", "example.test/deep"}; !slices.Equal(drifted, want) {
		t.Errorf("drift reported for %v, want %v", drifted, want)
	}
}
