package handlers_test

import (
	"os"
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

// The run removes crash leftovers older than a day and records unused for
// cache.retention (20261114-095140-d-cpt-ad4, rules 6 and 7).
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
