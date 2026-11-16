package handlers_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
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
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc), State: state,
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
	f.loc.MixedXDG = "XDG_CONFIG_HOME set, XDG_STATE_HOME not"
	state := &recordingStateSweep{}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc), State: state,
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
