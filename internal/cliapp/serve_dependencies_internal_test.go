package cliapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// A dependency whose clone vanished under a running server is cloned and
// served afresh, never from the runtime over the gone directory.
func TestConnectedDependenciesRebuildAVanishedClone(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	const repoID = "github.com/acme/platform"
	config := "repos:\n  - repo_id: " + repoID + "\n    clone_url: " + filepath.Join(root, "remote") + "\n"
	if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cloneGit := &synctest.CloneGit{Files: map[string]string{".sdd/config.yaml": "repo_id: " + repoID + "\n"}}
	registry := repos.NewRegistry(loc)
	builds := 0
	deps := &connectedDependencies{
		registry: registry,
		manager:  repos.NewManager(registry, repos.NewGraphClones(loc, cloneGit, synctest.Gate(loc.StateRoot, nil), synctest.CloneLocks{})),
		build: func(sdd.RepoID, string, *model.PerRepoConfig) (*sdd.ProjectRuntime, error) {
			builds++
			return &sdd.ProjectRuntime{}, nil
		},
	}
	first, err := deps.resolve(t.Context(), repoID)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := registry.CloneDir(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	second, err := deps.resolve(t.Context(), repoID)
	if err != nil {
		t.Fatalf("a vanished clone should be cloned again: %v", err)
	}
	if second == first || builds != 2 || cloneGit.Clones != 2 {
		t.Errorf("the vanished clone should be cloned and its runtime built again, builds %d, clones %d", builds, cloneGit.Clones)
	}
	if !repos.IsCloned(dir) {
		t.Error("the clone should stand again")
	}
}

// A dependency clone the git gate defers names the command that makes it.
func TestConnectedDependenciesNameTheFixForADeferredClone(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	const repoID = "github.com/acme/platform"
	config := "repos:\n  - repo_id: " + repoID + "\n    clone_url: git@github.com:acme/platform.git\n"
	if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cloneGit := &synctest.CloneGit{Files: map[string]string{".sdd/config.yaml": "repo_id: " + repoID + "\n"}, Remote: "git@github.com:acme/platform.git"}
	gate := gitgate.New(gitgate.Options{StateDir: loc.StateRoot, Limits: func(string) gitgate.Limits {
		limits := model.DefaultGitLimits()
		limits.SSHPerHour = 0
		return limits
	}})
	registry := repos.NewRegistry(loc)
	deps := &connectedDependencies{
		registry: registry,
		manager:  repos.NewManager(registry, repos.NewGraphClones(loc, cloneGit, gate, synctest.CloneLocks{})),
		build: func(sdd.RepoID, string, *model.PerRepoConfig) (*sdd.ProjectRuntime, error) {
			return &sdd.ProjectRuntime{}, nil
		},
	}
	_, err := deps.resolve(t.Context(), repoID)
	var appErr *sdd.ApplicationError
	if !errors.As(err, &appErr) || appErr.Code != sdd.ErrorProjectUnavailable || !strings.Contains(appErr.Message, "sdd repo sync "+repoID) {
		t.Errorf("a deferred clone should be unavailable with the fix named, got %v", err)
	}
}
