package handlers_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/cacheprune"
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
		// undropped leaves out the record `sdd repo remove` writes.
		undropped bool
		removed   bool
	}{
		{name: "a clone nothing uses goes", repo: "unused", removed: true},
		{name: "a clone no repo remove dropped stays", repo: "unlisted", undropped: true},
		{name: "a connected repo's clone stays", repo: "kept"},
		{
			name: "a clone holding unpushed commits stays", repo: "unpushed",
			prepare: func(t *testing.T, _ repos.Locations, dir string, _ gitgate.Target) func() {
				gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "sdd: not pushed")
				return nil
			},
		},
		{
			name: "a clone holding entries a force push set aside stays", repo: "setaside",
			prepare: func(t *testing.T, _ repos.Locations, dir string, _ gitgate.Target) func() {
				entry := filepath.Join(dir, ".git", "sdd-set-aside", "files", ".sdd", "graph", "2026", "11", "07-120000-s-tac-abc.md")
				if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(entry, []byte("set aside\n"), 0o644); err != nil {
					t.Fatal(err)
				}
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
			if !tc.undropped {
				if err := repos.NewManager(repos.NewRegistry(loc), nil).MarkDisconnected(model.RepoID("github.com/acme/" + tc.repo)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.prepare != nil {
				if release := tc.prepare(t, loc, dir, target); release != nil {
					defer release()
				}
			}

			var counts model.CachePruneCounts
			h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
				CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc),
				OwnedClones: syncstate.OwnedClones{Gate: synctest.Gate(loc.StateRoot, nil)}, Gate: synctest.Gate(loc.StateRoot, nil),
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

// A dropped clone holding a connected one stays with it, and the run goes on.
func TestCacheHandlerPruneKeepsADroppedCloneHoldingAConnectedOne(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{
		ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"),
		StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data"),
	}
	config := "repos:\n  - repo_id: github.com/acme/foo/bar\n    clone_url: git@github.com:acme/foo/bar.git\n"
	if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	outer, _ := ownedClone(t, loc, "foo")
	inner, _ := ownedClone(t, loc, "foo/bar")
	if err := repos.NewManager(repos.NewRegistry(loc), nil).MarkDisconnected("github.com/acme/foo"); err != nil {
		t.Fatal(err)
	}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc),
		OwnedClones: syncstate.OwnedClones{Gate: synctest.Gate(loc.StateRoot, nil)}, Gate: synctest.Gate(loc.StateRoot, nil),
	})
	if err := h.Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatalf("a clone kept for the clone it holds should not fail the run: %v", err)
	}
	if !exists(t, filepath.Join(inner, ".git")) || !exists(t, filepath.Join(outer, ".git")) {
		t.Error("a dropped clone holding a connected one should stay, and the connected one with it")
	}
}

// A process whose config root holds no repos list while it reaches this
// machine's clones keeps them all.
func TestCacheHandlerPruneKeepsClonesAForeignConfigLacks(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{
		ConfigPath: filepath.Join(root, "foreign", "config.yaml"), CacheRoot: filepath.Join(root, "cache"),
		StateRoot: filepath.Join(root, "foreign-state"), DataRoot: filepath.Join(root, "data"),
	}
	first, _ := ownedClone(t, loc, "graph")
	second, _ := ownedClone(t, loc, "platform")
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc),
		OwnedClones: syncstate.OwnedClones{Gate: synctest.Gate(loc.StateRoot, nil)}, Gate: synctest.Gate(loc.StateRoot, nil),
	})
	if err := h.Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if !exists(t, first) || !exists(t, second) {
		t.Error("clones no repo remove dropped should stay whatever the loaded repos list holds")
	}
}

// A disconnected clone meeting a missing config file fails the run, recorded
// for the next command to report, and stays.
func TestCacheHandlerPruneRefusesAMissingConfig(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{
		ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"),
		StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data"),
	}
	dir, _ := ownedClone(t, loc, "dropped")
	if err := repos.NewManager(repos.NewRegistry(loc), nil).MarkDisconnected("github.com/acme/dropped"); err != nil {
		t.Fatal(err)
	}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc),
		OwnedClones: syncstate.OwnedClones{Gate: synctest.Gate(loc.StateRoot, nil)}, Gate: synctest.Gate(loc.StateRoot, nil),
	})
	err := h.Prune(t.Context(), &command.PruneCacheCmd{})
	if !errors.Is(err, repos.ErrClonePassRefused) {
		t.Errorf("the run should fail with the refused clone pass, got %v", err)
	}
	if !exists(t, dir) {
		t.Error("a refused clone pass should remove nothing")
	}
	status, err := cacheprune.Load(loc.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Error, repos.ErrClonePassRefused.Error()) {
		t.Errorf("the refusal should be recorded for the next command, got %q", status.Error)
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

// The run packs a listed clone with git gc --auto.
func TestCacheHandlerPrunePacksListedClones(t *testing.T) {
	f := newPruneFixture(t, "")
	dir, _ := ownedClone(t, f.loc, "kept")
	gitIn(t, dir, "repack", "-q")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "sdd: second")
	gitIn(t, dir, "repack", "-q")
	gitIn(t, dir, "config", "gc.autoPackLimit", "1")
	gitIn(t, dir, "config", "gc.autoDetach", "false")
	packs := func() int {
		t.Helper()
		found, err := filepath.Glob(filepath.Join(dir, ".git", "objects", "pack", "*.pack"))
		if err != nil {
			t.Fatal(err)
		}
		return len(found)
	}
	if packs() < 2 {
		t.Fatal("the clone should start with more packs than gc.autoPackLimit")
	}
	release, err := syncstate.OwnedClones{}.TryLockSync(t.Context(), dir)
	if err != nil || release == nil {
		t.Fatalf("taking the sync lock: %v", err)
	}
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatal(err)
	}
	release()
	if packs() < 2 {
		t.Error("a clone whose sync lock is held should be left unpacked")
	}
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatal(err)
	}
	if packs() != 1 {
		t.Errorf("git gc --auto should have joined the listed clone's packs, %d left", packs())
	}
	if !exists(t, dir) {
		t.Error("a listed clone should stay")
	}
}
