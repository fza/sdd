package handlers_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/syncstate"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// ownedClone makes an sdd-owned clone of github.com/acme/<name> whose one
// commit its remote-tracking branch holds.
func ownedClone(t *testing.T, loc repos.Locations, name string) (dir string, target gitgate.Target) {
	t.Helper()
	dir = filepath.Join(loc.DataRoot, "repos", "github.com", "acme", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	url := "git@github.com:acme/" + name + ".git"
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "sdd: first")
	gitIn(t, dir, "remote", "add", "origin", url)
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	target, err := gitgate.TargetFromURL(url)
	if err != nil {
		t.Fatal(err)
	}
	return dir, target
}

func recordFetch(t *testing.T, loc repos.Locations, target gitgate.Target, at time.Time) {
	t.Helper()
	gate := synctest.Gate(loc.StateRoot, func() time.Time { return at })
	if err := gate.Run(t.Context(), target, gitgate.Fetch, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestCacheHandlerPruneOwnedClones(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		repo    string
		prepare func(t *testing.T, loc repos.Locations, dir string, target gitgate.Target) (release func())
		removed bool
	}{
		{name: "a clone nothing uses goes", repo: "unused", removed: true},
		{name: "a connected repo's clone stays", repo: "kept"},
		{
			name: "a clone holding unpushed commits stays", repo: "unpushed",
			prepare: func(t *testing.T, _ repos.Locations, dir string, _ gitgate.Target) func() {
				gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "sdd: not pushed")
				return nil
			},
		},
		{
			name: "a clone whose sync lock is held stays", repo: "syncing",
			prepare: func(t *testing.T, _ repos.Locations, dir string, _ gitgate.Target) func() {
				release, err := syncstate.OwnedClones{}.TryLockSync(t.Context(), dir)
				if err != nil || release == nil {
					t.Fatalf("taking the sync lock: %v", err)
				}
				return release
			},
		},
		{
			name: "a clone git reached within the retention stays", repo: "synced",
			prepare: func(t *testing.T, loc repos.Locations, _ string, target gitgate.Target) func() {
				recordFetch(t, loc, target, now.Add(-24*time.Hour))
				return nil
			},
		},
		{
			name: "a clone git last reached before the retention goes", repo: "abandoned", removed: true,
			prepare: func(t *testing.T, loc repos.Locations, _ string, target gitgate.Target) func() {
				recordFetch(t, loc, target, now.Add(-pruneRetention-time.Hour))
				return nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			loc := repos.Locations{
				ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"),
				StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data"),
			}
			config := "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"
			if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			dir, target := ownedClone(t, loc, tc.repo)
			if tc.prepare != nil {
				if release := tc.prepare(t, loc, dir, target); release != nil {
					defer release()
				}
			}

			var counts model.CachePruneCounts
			h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
				CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc),
				OwnedClones: syncstate.OwnedClones{Gate: synctest.Gate(loc.StateRoot, nil)},
			})
			if err := h.Prune(t.Context(), &command.PruneCacheCmd{OnPruned: func(c model.CachePruneCounts) { counts = c }}); err != nil {
				t.Fatalf("pruning: %v", err)
			}
			if gone := !exists(t, dir); gone != tc.removed {
				t.Errorf("clone removed = %v, want %v", gone, tc.removed)
			}
			if tc.removed && counts.Clones != 1 {
				t.Errorf("the removed clone should be counted, got %+v", counts)
			}
		})
	}
}

func TestCacheHandlerPruneFailsWithoutTheOwnedCloneProbe(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{
		ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"),
		StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data"),
	}
	dir, _ := ownedClone(t, loc, "unprobed")
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc)})
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("a run that cannot judge an sdd-owned clone should fail, panicked: %v", recovered)
		}
	}()
	if err := h.Prune(t.Context(), &command.PruneCacheCmd{}); err == nil {
		t.Error("a run that cannot judge an sdd-owned clone should fail")
	}
	if !exists(t, dir) {
		t.Error("a clone nothing judged should stay")
	}
}
