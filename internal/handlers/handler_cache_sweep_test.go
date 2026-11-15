package handlers_test

import (
	"context"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// recordingStateSweep records the retentions a pass sweeps the state folder
// under and reports swept.
type recordingStateSweep struct {
	session, unused time.Duration
	swept           model.CachePruneCounts
}

func (s *recordingStateSweep) Sweep(_ context.Context, _ time.Time, session, unused time.Duration) (model.CachePruneCounts, error) {
	s.session, s.unused = session, unused
	return s.swept, nil
}

// The run sweeps the state folder under the user-global sessions.retention
// and cache.retention, and counts what that sweep removed.
func TestCacheHandlerPruneSweepsTheStateFolder(t *testing.T) {
	f := newPruneFixture(t, "sessions:\n  retention: 1d\ncache:\n  retention: 30d\n")
	state := &recordingStateSweep{swept: model.CachePruneCounts{Sessions: 2, Unreadable: 1, Bytes: 10}}
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
	if counts.Sessions != 2 || counts.Unreadable != 1 || counts.Clones != 1 {
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
