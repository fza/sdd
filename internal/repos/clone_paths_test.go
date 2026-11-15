package repos_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func clonePathLocations(t *testing.T) repos.Locations {
	t.Helper()
	root := t.TempDir()
	return repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), DataRoot: filepath.Join(root, "data")}
}

func clonePaths(reg *repos.Registry) map[string]func(model.RepoID) (string, error) {
	return map[string]func(model.RepoID) (string, error){"clone": reg.CloneDir, "legacy cache": reg.LegacyCacheDir}
}

func TestClonePathsStayInsideTheirRoot(t *testing.T) {
	reg := repos.NewRegistry(clonePathLocations(t))
	for name, path := range clonePaths(reg) {
		for _, id := range []model.RepoID{"evil.example/../..", "evil.example/..", "evil.example/../../etc", "evil.example/./x"} {
			dir, err := path(id)
			if err == nil {
				t.Errorf("the %s path of %q should be refused, got %q", name, id, dir)
			}
		}
	}
}

func TestClonePathsRefuseASymlinkBelowTheRoot(t *testing.T) {
	loc := clonePathLocations(t)
	outside := filepath.Join(filepath.Dir(loc.CacheRoot), "victim")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{filepath.Join(loc.DataRoot, "repos"), loc.CacheRoot} {
		clone := filepath.Join(base, "evil.example", "a")
		if err := os.MkdirAll(clone, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(clone, "l")); err != nil {
			t.Fatal(err)
		}
	}
	reg := repos.NewRegistry(loc)
	for name, path := range clonePaths(reg) {
		for _, id := range []model.RepoID{"evil.example/a/l", "evil.example/a/l/x"} {
			dir, err := path(id)
			if err == nil {
				t.Errorf("the %s path of %q reaches through a symlink and should be refused, got %q", name, id, dir)
			}
		}
		if _, err := path("evil.example/a/b"); err != nil {
			t.Errorf("a %s path below a plain directory should resolve: %v", name, err)
		}
	}
}

func TestAddRepoRefusesOverlappingIDs(t *testing.T) {
	cfg := &repos.GlobalConfig{}
	if err := cfg.AddRepo(repos.ConnectedRepo{RepoID: "github.com/org/repo", CloneURL: "git@github.com:org/repo.git"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []model.RepoID{"github.com/ORG/repo", "github.com/org", "github.com/org/repo/.git"} {
		if err := cfg.AddRepo(repos.ConnectedRepo{RepoID: id, CloneURL: "/srv/" + string(id)}); err == nil {
			t.Errorf("adding %q should be refused, it overlaps a connected repo", id)
		}
	}
	if err := cfg.AddRepo(repos.ConnectedRepo{RepoID: "github.com/org/repository", CloneURL: "/srv/repository"}); err != nil {
		t.Errorf("an id sharing only a name prefix should connect: %v", err)
	}
}
