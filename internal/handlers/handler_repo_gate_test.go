package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/repos/repostest"
)

// pullCountingGit counts the pulls that reach git.
type pullCountingGit struct{ pulls int }

func (g *pullCountingGit) Clone(_ context.Context, _, dir string) error {
	return os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
}

func (g *pullCountingGit) PullFFOnly(context.Context, string) error {
	g.pulls++
	return nil
}

func (g *pullCountingGit) ExpandURL(_ context.Context, url string) (string, error) { return url, nil }

const gatedRepoID = model.RepoID("example.test/acme/other")

// gatedConnection is a handler whose one connected repository lives on a
// host the gate limits, its cache already cloned.
func gatedConnection(t *testing.T) (*handlers.Handler, *pullCountingGit) {
	t.Helper()
	dir := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(dir, "config.yaml"), CacheRoot: filepath.Join(dir, "cache")}
	cfg := &repos.GlobalConfig{}
	err := cfg.AddRepo(repos.ConnectedRepo{RepoID: gatedRepoID, CloneURL: "https://example.test/acme/other.git"})
	if err != nil {
		t.Fatal(err)
	}
	repostest.WriteConfig(t, loc.ConfigPath, cfg)
	reg := repos.NewRegistry(loc)
	cacheDir, err := reg.CacheDir(gatedRepoID)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(filepath.Join(cacheDir, ".git"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	git := &pullCountingGit{}
	return handlers.New(handlers.Options{Repos: repos.NewManager(reg, git, synctest.Gate(t.TempDir(), func() time.Time { return now }))}), git
}

func TestEnsureReposFreshLeavesADeferredCacheAsItStands(t *testing.T) {
	h, git := gatedConnection(t)
	fresh := func() (bool, []model.Phase) {
		var phases []model.Phase
		changed, err := h.EnsureReposFresh(t.Context(), command.EnsureReposFreshCmd{
			RepoIDs: []model.RepoID{gatedRepoID}, OnPhase: func(phase model.Phase) { phases = append(phases, phase) },
		})
		if err != nil {
			t.Fatalf("a refresh should never fail on a deferral: %v", err)
		}
		return changed, phases
	}

	changed, _ := fresh()
	if !changed || git.pulls != 1 {
		t.Fatalf("the first refresh should pull, got changed %t after %d pulls", changed, git.pulls)
	}
	changed, phases := fresh()
	if changed || git.pulls != 1 {
		t.Errorf("a refresh within the fetch cooldown should not pull, got changed %t after %d pulls", changed, git.pulls)
	}
	if len(phases) != 0 {
		t.Errorf("a refresh not due should report no phase, got %v", phases)
	}
}

func TestRepoSyncReportsADeferredRepo(t *testing.T) {
	h, git := gatedConnection(t)
	err := h.RepoSync(t.Context(), &command.RepoSyncCmd{})
	if err != nil {
		t.Fatal(err)
	}
	var synced []model.RepoID
	var deferredUntil time.Time
	err = h.RepoSync(t.Context(), &command.RepoSyncCmd{
		OnSynced:   func(repoID model.RepoID) { synced = append(synced, repoID) },
		OnDeferred: func(_ model.RepoID, next time.Time) { deferredUntil = next },
	})
	if err != nil {
		t.Fatalf("a deferred repo should be reported, not failed: %v", err)
	}
	if git.pulls != 1 || len(synced) != 0 {
		t.Errorf("a sync within the fetch cooldown should not pull, got %d pulls, synced %v", git.pulls, synced)
	}
	if deferredUntil.IsZero() {
		t.Error("the deferral should name when the repo may sync")
	}
}
