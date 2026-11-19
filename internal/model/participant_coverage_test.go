package model_test

import (
	"sync"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// Graph health knows a participant named by any alias of an actor, as it
// knows one named by a canonical.
func TestParticipantCoverageAcceptsActorAliases(t *testing.T) {
	actor := personEntry("20260410-120000-s-prc-aaa")
	actor.Kind, actor.Canonical, actor.Aliases = model.KindActor, "Christopher", []string{"Chris"}
	for _, tc := range []struct {
		participant string
		known       bool
	}{
		{"Christopher", true},
		{"Chris", true},
		{"Claude", false},
	} {
		t.Run(tc.participant, func(t *testing.T) {
			e := personEntry("20260410-130000-s-cpt-bbb")
			e.Participants = []string{tc.participant}
			g := model.NewGraph([]*model.Entry{actor, e})
			warned := false
			for _, w := range g.WarningsFor(e) {
				warned = warned || w.Field == "participants" && w.Value == tc.participant
			}
			if warned == tc.known {
				t.Errorf("participant %q: coverage warning %v, want %v", tc.participant, warned, !tc.known)
			}
		})
	}
}

// A graph built over entries an earlier graph validated reports the same
// warnings, not the earlier graph's plus its own (as a composition assembling
// a fresh graph per serve does).
func TestGraphRebuiltOverSameEntriesKeepsWarnings(t *testing.T) {
	e := personEntry("20260410-130000-s-cpt-bbb")
	e.Participants = []string{"Claude"}
	e.Warnings = []model.Warning{{Field: "topics", Value: "x", Message: "read-time warning"}}
	entries := []*model.Entry{e}
	first := model.NewGraph(append([]*model.Entry(nil), entries...)).Health()
	if first.Warnings != 2 {
		t.Fatalf("first graph: %d warning(s), want the read-time and the coverage warning: %+v", first.Warnings, first.Issues)
	}
	for i := range 3 {
		if got := model.NewGraph(append([]*model.Entry(nil), entries...)).Health(); got.Warnings != first.Warnings || len(got.Issues) != len(first.Issues) {
			t.Errorf("rebuild %d: %d warning(s), want %d: %+v", i+1, got.Warnings, first.Warnings, got.Issues)
		}
	}
}

// Graphs built at the same time over the same entries (cached snapshots share
// them) each report their own warnings, without writing to the shared entries.
func TestGraphsBuiltConcurrentlyOverSameEntries(t *testing.T) {
	e := personEntry("20260410-130000-s-cpt-bbb")
	e.Participants = []string{"Claude"}
	e.Closes = []string{"20260410-110000-s-cpt-zzz"}
	e.Warnings = []model.Warning{{Field: "topics", Value: "x", Message: "read-time warning"}}
	entries := []*model.Entry{e}
	want := model.NewGraph(append([]*model.Entry(nil), entries...)).Health()
	if want.Warnings != 3 {
		t.Fatalf("graph: %d warning(s), want the read-time, coverage and dangling-closes warning: %+v", want.Warnings, want.Issues)
	}
	healths := make([]model.GraphHealth, 8)
	var wg sync.WaitGroup
	for i := range healths {
		wg.Go(func() {
			healths[i] = model.NewGraph(append([]*model.Entry(nil), entries...)).Health()
		})
	}
	wg.Wait()
	for i, got := range healths {
		if got.Warnings != want.Warnings || len(got.Issues) != len(want.Issues) {
			t.Errorf("graph %d: %d warning(s), want %d: %+v", i, got.Warnings, want.Warnings, got.Issues)
		}
	}
	if len(e.Warnings) != 1 {
		t.Errorf("entry carries %d warning(s) after validation, want only its read-time one: %+v", len(e.Warnings), e.Warnings)
	}
}
