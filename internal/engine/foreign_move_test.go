package engine_test

import (
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

func patchSpec(t *testing.T) *engine.Spec {
	t.Helper()
	spec, err := engine.ParseSpec(patchEntry(t, patchMachine))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func newPatchEngine() *engine.Engine {
	return engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
}

// earlierBuildLog is a log as a build before the procedure revision writes
// it: a move started and reported at its draft step, its started event
// carrying no revision.
func earlierBuildLog() []engine.Event {
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	return []engine.Event{
		{V: 1, TS: at, Session: "s_other", Seq: 1, Event: engine.EventSessionMeta, Data: map[string]any{"participant": "tester"}},
		{V: 1, TS: at, Session: "s_other", Seq: 2, Instance: "i_1", Event: engine.EventStarted, Data: map[string]any{"procedure": "patchproc", "entry": "20260826-130000-d-prc-pat", "step": "draft"}},
		{V: 1, TS: at, Session: "s_other", Seq: 3, Instance: "i_1", Event: engine.EventReport, Data: map[string]any{"step": "draft", "fields": map[string]any{"body": "alpha"}}},
	}
}

func TestAMoveAnEarlierBuildStartedOffersOnlyAbandon(t *testing.T) {
	spec := patchSpec(t)
	resolve := func(string) (*engine.Spec, error) { return spec, nil }
	sink := &recordingSink{}
	sess, err := newPatchEngine().ReplaySession("s_other", "tester", earlierBuildLog(), resolve, sink)
	if err != nil {
		t.Fatal(err)
	}

	sv, err := sess.Serve("i_1")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Goal != engine.ForeignMoveGoal || sv.ReportSchema != nil || sv.Chooser != nil || len(sv.Lanes) != 0 || !strings.Contains(sv.Instructions, "abandon") {
		t.Fatalf("a move an earlier build started should serve abandon alone, got %+v", sv)
	}
	if _, err := sess.Report("i_1", map[string]any{"goal": "done"}); err == nil || !strings.Contains(err.Error(), "abandon") {
		t.Errorf("a report should be refused naming abandon, got %v", err)
	}
	if _, err := sess.Answer("i_1", "", "confirm", nil, ""); err == nil || !strings.Contains(err.Error(), "abandon") {
		t.Errorf("an answer should be refused naming abandon, got %v", err)
	}
	if err := sess.Park("i_1", "later"); err == nil || !strings.Contains(err.Error(), "abandon") {
		t.Errorf("a park should be refused naming abandon, got %v", err)
	}
	if len(sink.events) != 0 {
		t.Errorf("refused calls should log nothing, got %+v", sink.events)
	}
	if err := sess.Abandon("i_1", "an earlier build started it"); err != nil {
		t.Fatalf("abandon should end it, got %v", err)
	}
}

func TestAMoveThisBuildStartsContinuesBesideAnEarlierOne(t *testing.T) {
	spec := patchSpec(t)
	resolve := func(string) (*engine.Spec, error) { return spec, nil }
	sink := &recordingSink{}
	sess, err := newPatchEngine().ReplaySession("s_other", "tester", earlierBuildLog(), resolve, sink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Start(spec, map[string]any{"body": "beta"}, ""); err != nil {
		t.Fatal(err)
	}

	replayed, err := newPatchEngine().ReplaySession("s_other", "tester", append(earlierBuildLog(), sink.events...), resolve, nil)
	if err != nil {
		t.Fatal(err)
	}
	sv, err := replayed.Serve("i_2")
	if err != nil {
		t.Fatal(err)
	}
	if sv.ReportSchema == nil || sv.Goal == engine.ForeignMoveGoal {
		t.Errorf("a move this build started should serve its step after a replay, got %+v", sv)
	}
	if held, _ := replayed.Serve("i_1"); held.Goal != engine.ForeignMoveGoal {
		t.Errorf("the move the earlier build started should stay abandon-only, got %+v", held)
	}
}
