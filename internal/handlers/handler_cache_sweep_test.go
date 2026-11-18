package handlers_test

import (
	"errors"
	"os"
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

// The run removes crash leftovers older than a day and records unused for
// cache.retention.
func TestCacheHandlerPruneSweepsLeftoversAndRecords(t *testing.T) {
	f := newPruneFixture(t, "")
	now := time.Now()
	dayOld := now.Add(-model.CacheLeftoverAge - time.Hour)
	unused := now.Add(-pruneRetention - time.Hour)
	write := func(path string, at time.Time) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
		return path
	}
	oldTrace := write(filepath.Join(f.loc.StateRoot, "traces", "1.json"), dayOld)
	liveTrace := write(filepath.Join(f.loc.StateRoot, "traces", "2.json"), now)
	staging := filepath.Join(f.loc.DataRoot, "repos", ".adding-1")
	write(filepath.Join(staging, "clone", "HEAD"), dayOld)
	if err := os.Chtimes(filepath.Join(staging, "clone"), dayOld, dayOld); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(staging, dayOld, dayOld); err != nil {
		t.Fatal(err)
	}
	oldCalibration := write(filepath.Join(f.loc.CacheRoot, "calibration", "calibration-0001.json"), unused)
	usedCalibration := write(filepath.Join(f.loc.CacheRoot, "calibration", "calibration-0002.json"), now)
	stale, err := gitgate.TargetFromURL("git@github.com:acme/stale.git")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := gitgate.TargetFromURL("git@github.com:acme/fresh.git")
	if err != nil {
		t.Fatal(err)
	}
	recordFetch(t, f.loc, stale, unused)
	recordFetch(t, f.loc, fresh, now.Add(-time.Hour))

	var counts model.CachePruneCounts
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{OnPruned: func(c model.CachePruneCounts) { counts = c }}); err != nil {
		t.Fatal(err)
	}
	for path, wantKept := range map[string]bool{oldTrace: false, liveTrace: true, staging: false, oldCalibration: false, usedCalibration: true} {
		if kept := exists(t, path); kept != wantKept {
			t.Errorf("%s kept should be %v", path, wantKept)
		}
	}
	gate := f.gate()
	for target, wantKept := range map[gitgate.Target]bool{stale: false, fresh: true} {
		last, err := gate.LastAttempt(t.Context(), target)
		if err != nil {
			t.Fatal(err)
		}
		if kept := !last.IsZero(); kept != wantKept {
			t.Errorf("the gate record of %s kept should be %v", target.Repo, wantKept)
		}
	}
	if counts.Leftovers != 2 || counts.Records != 2 {
		t.Errorf("the run should count two leftovers and two records, got %+v", counts)
	}
}

// A host folder under the cache root is where an sdd keeping its clones there
// places them: the run leaves it, also when it holds no clone.
func TestCacheHandlerPruneKeepsHostFoldersUnderTheCacheRoot(t *testing.T) {
	f := newPruneFixture(t, "")
	old := time.Now().Add(-model.CacheLeftoverAge - time.Hour)
	host := filepath.Join(f.loc.CacheRoot, "gitlab.example.org")
	lock := filepath.Join(host, "team", "graph", ".sdd", "graph", ".sdd-runtime", "graph.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(host, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, old, old)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatal(err)
	}

	if !exists(t, lock) {
		t.Error("a host folder under the cache root should stay with what it holds")
	}
}

// The run sweeps the state folder under the user-global sessions.retention
// and cache.retention, and counts what that sweep removed.
func TestCacheHandlerPruneSweepsTheStateFolder(t *testing.T) {
	f := newPruneFixture(t, "sessions:\n  retention: 1d\ncache:\n  retention: 30d\n")
	state := &synctest.StateSweep{Swept: model.CachePruneCounts{Sessions: 2, Projects: 1, Unreadable: 1, Bytes: 10}}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc),
		OwnedClones: syncstate.OwnedClones{Gate: f.gate()}, Gate: f.gate(), State: state,
	})
	var counts model.CachePruneCounts
	if err := h.Prune(t.Context(), &command.PruneCacheCmd{OnPruned: func(c model.CachePruneCounts) { counts = c }}); err != nil {
		t.Fatal(err)
	}
	if state.Session != 24*time.Hour || state.Unused != 30*24*time.Hour {
		t.Errorf("the state sweep should run under the global retentions, got %v and %v", state.Session, state.Unused)
	}
	if counts.Sessions != 2 || counts.Projects != 1 || counts.Unreadable != 1 {
		t.Errorf("the run should count what the state sweep removed, got %+v", counts)
	}
}

// State, cache and data directories from mixed sources may put the state root
// on another setup's folder: the run sweeps none of it and fails, recorded
// for the next command to report.
func TestCacheHandlerPruneRefusesTheStateSweepUnderMixedXDG(t *testing.T) {
	f := newPruneFixture(t, "")
	f.loc.MixedStateXDG = "XDG_CACHE_HOME, XDG_DATA_HOME set, XDG_STATE_HOME not"
	state := &synctest.StateSweep{}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc),
		OwnedClones: syncstate.OwnedClones{Gate: f.gate()}, Gate: f.gate(), State: state,
	})
	err := h.Prune(t.Context(), &command.PruneCacheCmd{Scheduled: true})
	if !errors.Is(err, handlers.ErrStateSweepRefused) {
		t.Errorf("the run should fail with the refused state sweep, got %v", err)
	}
	if state.Session != 0 || state.Unused != 0 {
		t.Error("a refused state sweep should not run")
	}
	status, err := cacheprune.Load(f.loc.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Error, handlers.ErrStateSweepRefused.Error()) || status.FailureNotice() == "" {
		t.Errorf("the refusal should be recorded for the next command, got %+v", status)
	}
}
