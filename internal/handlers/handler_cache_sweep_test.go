package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/syncstate"
)

// recordingStateSweep records the retentions a pass sweeps the state folder
// under and reports swept.
type recordingStateSweep struct {
	called          bool
	session, unused time.Duration
	swept           model.CachePruneCounts
}

func (s *recordingStateSweep) Sweep(_ context.Context, _ time.Time, session, unused time.Duration) (model.CachePruneCounts, error) {
	s.called, s.session, s.unused = true, session, unused
	return s.swept, nil
}

// The run sweeps the state folder under the user-global sessions.retention
// and cache.retention, and counts what that sweep removed.
func TestCacheHandlerPruneSweepsTheStateFolder(t *testing.T) {
	f := newPruneFixture(t, "sessions:\n  retention: 1d\ncache:\n  retention: 30d\n")
	state := &recordingStateSweep{swept: model.CachePruneCounts{Sessions: 2, Projects: 1, Unreadable: 1, Bytes: 10}}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc), State: state, OwnedClones: syncstate.OwnedClones{Gate: f.gate()}, Gate: f.gate(),
	})
	var counts model.CachePruneCounts
	if err := h.Prune(t.Context(), &command.PruneCacheCmd{OnPruned: func(c model.CachePruneCounts) { counts = c }}); err != nil {
		t.Fatal(err)
	}
	if state.session != 24*time.Hour || state.unused != 30*24*time.Hour {
		t.Errorf("the state sweep should run under the global retentions, got %v and %v", state.session, state.unused)
	}
	if counts.Sessions != 2 || counts.Projects != 1 || counts.Unreadable != 1 || counts.Clones != 1 {
		t.Errorf("the run should count what the state sweep removed beside the rest, got %+v", counts)
	}
}

// An invalid sessions.retention fails the run, which records the failure.
func TestCacheHandlerPruneRefusesAnInvalidSessionRetention(t *testing.T) {
	f := newPruneFixture(t, "sessions:\n  retention: soon\n")
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{}); err == nil {
		t.Fatal("a run on an invalid sessions.retention should fail")
	}
	if !exists(t, f.staleClone) {
		t.Error("a run that cannot resolve its retentions should remove nothing")
	}
}

func TestCacheHandlerPruneFailsWithoutAStateSweeper(t *testing.T) {
	f := newPruneFixture(t, "")
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc)})
	if err := h.Prune(t.Context(), &command.PruneCacheCmd{}); err == nil {
		t.Error("a run that cannot sweep the state folder should fail, not skip it silently")
	}
}

// With the XDG base directories set only in part, the state root may be
// another setup's: the run sweeps none of it, fails, and records why.
func TestCacheHandlerPruneRefusesTheStateSweepUnderMixedXDG(t *testing.T) {
	f := newPruneFixture(t, "")
	f.loc.MixedStateXDG = "XDG_CACHE_HOME, XDG_DATA_HOME set, XDG_STATE_HOME not"
	state := &recordingStateSweep{}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc), State: state, OwnedClones: syncstate.OwnedClones{Gate: f.gate()}, Gate: f.gate(),
	})
	err := h.Prune(t.Context(), &command.PruneCacheCmd{Scheduled: true})
	if err == nil || !strings.Contains(err.Error(), "XDG_STATE_HOME") {
		t.Fatalf("the run should fail naming the mixed XDG setup, got %v", err)
	}
	if state.called {
		t.Error("the state folder should not be swept under mixed XDG base directories")
	}
	status, loadErr := cacheprune.Load(f.loc.StateRoot)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if status.FailureNotice() == "" {
		t.Errorf("the refusal should be recorded for the next command to report, got %+v", status)
	}
}

// Where the config file lives does not make the state root another setup's:
// with only XDG_CONFIG_HOME set, the state sweep runs.
func TestCacheHandlerPruneSweepsTheStateFolderWithOnlyTheConfigHomeSet(t *testing.T) {
	f := newPruneFixture(t, "")
	f.loc.MixedXDG = "XDG_CONFIG_HOME set, XDG_CACHE_HOME, XDG_STATE_HOME not"
	state := &recordingStateSweep{}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc), State: state, OwnedClones: syncstate.OwnedClones{Gate: f.gate()}, Gate: f.gate(),
	})
	// The clone pass keeps its own refusal under these mixed roots.
	if err := h.Prune(t.Context(), &command.PruneCacheCmd{Scheduled: true}); err != nil && strings.Contains(err.Error(), "state folder") {
		t.Errorf("the state sweep should not be refused, got %v", err)
	}
	if !state.called {
		t.Error("the state folder should be swept when only XDG_CONFIG_HOME is set")
	}
}

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
	if counts.Leftovers != 2 || counts.Records != 2 || counts.GateRecords != 1 {
		t.Errorf("the run should count two leftovers, two calibrations and one gate record, got %+v", counts)
	}
}
