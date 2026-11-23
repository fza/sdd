package handlers_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func TestInit_RefusesToMigrateTheIndexUnderAnInvalidRepoID(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	sddDir := filepath.Join(repoRoot, model.SDDDirName)
	if err := os.MkdirAll(sddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("repo_id: evil.example/../../../escaped\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := &index.Manifest{Version: 1, Entries: map[string]index.EntryState{
		"20260101-120000-s-tac-abc": {Versions: []index.EntryVersion{{Hash: "h", Fingerprint: "fp"}}},
	}}
	if err := manifest.Save(filepath.Join(sddDir, "index")); err != nil {
		t.Fatal(err)
	}
	loc := repos.Locations{
		ConfigPath: filepath.Join(root, "config", "sdd", "config.yaml"),
		CacheRoot:  filepath.Join(root, "cache"), DataRoot: filepath.Join(root, "data"),
	}
	h := handlers.New(handlers.Options{
		Reader: finders.New(finders.Options{}),
		Repos:  repos.NewManager(repos.NewRegistry(loc), nil),
	})

	err := h.Init(t.Context(), &command.InitCmd{
		RepoRoot:      repoRoot,
		BinaryVersion: "v0.2.0",
		Targets:       []model.AgentTarget{model.AgentClaude},
		Scope:         model.ScopeProject,
	})
	if err == nil {
		t.Error("init should refuse a repo_id with parent segments")
	}
	if _, statErr := os.Stat(filepath.Join(root, "escaped")); !os.IsNotExist(statErr) {
		t.Errorf("init should create nothing outside the cache root: %v", statErr)
	}
}
