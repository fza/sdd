package repos_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func TestCacheDirStaysInsideTheCacheRoot(t *testing.T) {
	reg := repos.NewRegistry(repos.Locations{CacheRoot: t.TempDir()})
	for _, id := range []model.RepoID{"evil.example/../..", "evil.example/..", "evil.example/../../etc"} {
		dir, err := reg.CacheDir(id)
		if err == nil {
			t.Errorf("CacheDir(%q) = %q, want a refusal", id, dir)
		}
	}
}

func TestCacheDirRefusesASymlinkBelowTheRoot(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "cache")
	clone := filepath.Join(cacheRoot, "evil.example", "a")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "victim")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(clone, "l")); err != nil {
		t.Fatal(err)
	}
	reg := repos.NewRegistry(repos.Locations{CacheRoot: cacheRoot})

	for _, id := range []model.RepoID{"evil.example/a/l", "evil.example/a/l/x"} {
		dir, err := reg.CacheDir(id)
		if err == nil {
			t.Errorf("CacheDir(%q) = %q, want a refusal", id, dir)
		}
	}
	if _, err := reg.CacheDir("evil.example/a/b"); err != nil {
		t.Errorf("a path below a plain directory should resolve: %v", err)
	}
}

func TestAddRepoRefusesOverlappingIDs(t *testing.T) {
	cfg := &repos.GlobalConfig{}
	if err := cfg.AddRepo(repos.ConnectedRepo{RepoID: "github.com/org/repo", CloneURL: "git@github.com:org/repo.git"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []model.RepoID{"github.com/ORG/repo", "github.com/org", "github.com/org/repo/.git"} {
		if err := cfg.AddRepo(repos.ConnectedRepo{RepoID: id, CloneURL: "/srv/" + string(id)}); err == nil {
			t.Errorf("AddRepo(%q) should refuse an id overlapping a connected repo", id)
		}
	}
	if err := cfg.AddRepo(repos.ConnectedRepo{RepoID: "github.com/org/repository", CloneURL: "/srv/repository"}); err != nil {
		t.Errorf("an id sharing only a name prefix should connect: %v", err)
	}
}
