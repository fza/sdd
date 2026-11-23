package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/repos/repostest"
)

// declaringGit clones a repo whose committed config declares the repo_id
// mapped to its URL.
type declaringGit struct{ declares map[string]model.RepoID }

func (g declaringGit) Clone(_ context.Context, url, dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".sdd"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".sdd", "config.yaml"), []byte("repo_id: "+string(g.declares[url])+"\n"), 0o644)
}

func (declaringGit) PullFFOnly(context.Context, string) error { return nil }

func TestRepoAdd_RefusesAnIDOverlappingAConnectedClone(t *testing.T) {
	tests := []struct {
		name     string
		declared model.RepoID
	}{
		{"case variant", "github.com/ORG/repo"},
		{"parent of the clone", "github.com/org"},
		{"inside the clone", "github.com/org/repo/.git"},
		{"through a symlink in the clone", "github.com/org/repo/l/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			loc := repos.Locations{
				ConfigPath: filepath.Join(root, "config", "sdd", "config.yaml"),
				CacheRoot:  filepath.Join(root, "cache"),
			}
			reg := repos.NewRegistry(loc)
			cfg := &repos.GlobalConfig{}
			if err := cfg.AddRepo(repos.ConnectedRepo{RepoID: "github.com/org/repo", CloneURL: "git@github.com:org/repo.git"}); err != nil {
				t.Fatal(err)
			}
			repostest.WriteConfig(t, loc.ConfigPath, cfg)
			clone, err := reg.CacheDir("github.com/org/repo")
			if err != nil {
				t.Fatal(err)
			}
			precious := filepath.Join(clone, ".git", "precious")
			victim := filepath.Join(root, "victim", "x", "precious")
			for _, file := range []string{precious, victim} {
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(filepath.Join(root, "victim"), filepath.Join(clone, "l")); err != nil {
				t.Fatal(err)
			}
			const url = "/srv/crafted"
			h := handlers.New(handlers.Options{Repos: repos.NewManager(reg, declaringGit{declares: map[string]model.RepoID{url: tt.declared}})})

			err = h.RepoAdd(context.Background(), &command.RepoAddCmd{CloneURL: url})
			if err == nil {
				t.Error("RepoAdd should refuse an id overlapping a connected repo")
			}
			for _, file := range []string{precious, victim} {
				if _, err := os.Stat(file); err != nil {
					t.Errorf("RepoAdd should leave %s in place: %v", file, err)
				}
			}
		})
	}
}

func TestRepoAdd_ReplacesAStaleCacheWithTheClone(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{
		ConfigPath: filepath.Join(root, "config", "sdd", "config.yaml"),
		CacheRoot:  filepath.Join(root, "cache"),
	}
	reg := repos.NewRegistry(loc)
	repostest.WriteConfig(t, loc.ConfigPath, &repos.GlobalConfig{})
	cacheDir, err := reg.CacheDir("github.com/org/repo")
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(cacheDir, "stale")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	const url = "/srv/repo"
	h := handlers.New(handlers.Options{Repos: repos.NewManager(reg, declaringGit{declares: map[string]model.RepoID{url: "github.com/org/repo"}})})

	if err := h.RepoAdd(context.Background(), &command.RepoAddCmd{CloneURL: url}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, ".sdd", "config.yaml")); err != nil {
		t.Errorf("the clone should sit in its cache dir: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the stale cache should be cleared: %v", err)
	}
}

func TestEnsureReposFresh_SkipsOverlappingReposAndRefreshesTheRest(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{
		ConfigPath: filepath.Join(root, "config", "sdd", "config.yaml"),
		CacheRoot:  filepath.Join(root, "cache"),
	}
	reg := repos.NewRegistry(loc)
	repostest.WriteConfig(t, loc.ConfigPath, &repos.GlobalConfig{Repos: []repos.ConnectedRepo{
		{RepoID: "github.com/org", CloneURL: "/srv/org"},
		{RepoID: "github.com/org/repo", CloneURL: "/srv/repo"},
		{RepoID: "github.com/other/repo", CloneURL: "/srv/other"},
	}})
	h := handlers.New(handlers.Options{Repos: repos.NewManager(reg, declaringGit{})})

	changed, err := h.EnsureReposFresh(context.Background(), command.EnsureReposFreshCmd{
		RepoIDs: []model.RepoID{"github.com/org", "github.com/org/repo", "github.com/other/repo"},
	})
	if err == nil || !strings.Contains(err.Error(), `"github.com/org"`) || !strings.Contains(err.Error(), `"github.com/org/repo"`) {
		t.Errorf("EnsureReposFresh should fail naming the overlapping pair, got %v", err)
	}
	if !changed {
		t.Error("EnsureReposFresh should report the disjoint repo's clone as a change")
	}
	for id, want := range map[model.RepoID]bool{"github.com/org": false, "github.com/org/repo": false, "github.com/other/repo": true} {
		dir, err := reg.CacheDir(id)
		if err != nil {
			t.Fatal(err)
		}
		if repos.IsCloned(dir) != want {
			t.Errorf("EnsureReposFresh should clone %s only when it overlaps no other repo", id)
		}
	}
}

func TestRepoSync_SkipsOverlappingReposAndSyncsTheRest(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{
		ConfigPath: filepath.Join(root, "config", "sdd", "config.yaml"),
		CacheRoot:  filepath.Join(root, "cache"),
	}
	reg := repos.NewRegistry(loc)
	repostest.WriteConfig(t, loc.ConfigPath, &repos.GlobalConfig{Repos: []repos.ConnectedRepo{
		{RepoID: "github.com/org", CloneURL: "/srv/org"},
		{RepoID: "github.com/org/repo", CloneURL: "/srv/repo"},
		{RepoID: "github.com/other/repo", CloneURL: "/srv/other"},
	}})
	h := handlers.New(handlers.Options{Repos: repos.NewManager(reg, declaringGit{})})

	var synced []model.RepoID
	err := h.RepoSync(context.Background(), &command.RepoSyncCmd{OnSynced: func(id model.RepoID) { synced = append(synced, id) }})
	if err == nil || !strings.Contains(err.Error(), `"github.com/org"`) || !strings.Contains(err.Error(), `"github.com/org/repo"`) {
		t.Errorf("RepoSync should fail naming the overlapping pair, got %v", err)
	}
	if !slices.Equal(synced, []model.RepoID{"github.com/other/repo"}) {
		t.Errorf("RepoSync should sync only the disjoint repo, synced %v", synced)
	}
	for _, id := range []model.RepoID{"github.com/org", "github.com/org/repo"} {
		dir, err := reg.CacheDir(id)
		if err != nil {
			t.Fatal(err)
		}
		if repos.IsCloned(dir) {
			t.Errorf("RepoSync should not clone the overlapping repo %s", id)
		}
	}
}
