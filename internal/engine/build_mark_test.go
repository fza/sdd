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

func TestNewSessionMarksItsLogRightAfterTheHeader(t *testing.T) {
	sink := &recordingSink{}
	newPatchEngine().NewSession("s_mark", "tester", sink)

	if len(sink.events) != 2 || sink.events[0].Event != engine.EventSessionMeta || sink.events[1].Event != engine.EventBuildMark {
		t.Fatalf("a new session's log should open with its header, then the build mark, got %+v", sink.events)
	}
}

// unmarkedLog is a log as a build without the mark writes it: a move started
// and reported at its draft step.
func unmarkedLog() []engine.Event {
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	return []engine.Event{
		{V: 1, TS: at, Session: "s_other", Seq: 1, Event: engine.EventSessionMeta, Data: map[string]any{"participant": "tester"}},
		{V: 1, TS: at, Session: "s_other", Seq: 2, Instance: "i_1", Event: engine.EventStarted, Data: map[string]any{"procedure": "patchproc", "entry": "20260826-130000-d-prc-pat", "step": "draft"}},
		{V: 1, TS: at, Session: "s_other", Seq: 3, Instance: "i_1", Event: engine.EventReport, Data: map[string]any{"step": "draft", "fields": map[string]any{"body": "alpha"}}},
	}
}

func TestAMoveStartedBeforeTheMarkOffersOnlyAbandon(t *testing.T) {
	spec := patchSpec(t)
	resolve := func(string) (*engine.Spec, error) { return spec, nil }
	sink := &recordingSink{}
	sess, err := newPatchEngine().ReplaySession("s_other", "tester", unmarkedLog(), resolve, sink)
	if err != nil {
		t.Fatal(err)
	}

	sv, err := sess.Serve("i_1")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Goal != engine.ForeignMoveGoal || sv.ReportSchema != nil || sv.Chooser != nil || len(sv.Lanes) != 0 || !strings.Contains(sv.Instructions, "abandon") {
		t.Fatalf("a move another build started should serve abandon alone, got %+v", sv)
	}
	if _, err := sess.Report("i_1", map[string]any{"goal": "done"}); err == nil || !strings.Contains(err.Error(), "abandon") {
		t.Errorf("a report should be refused naming abandon, got %v", err)
	}
	if err := sess.Park("i_1", "later"); err == nil || !strings.Contains(err.Error(), "abandon") {
		t.Errorf("a park should be refused naming abandon, got %v", err)
	}
	if len(sink.events) != 0 {
		t.Errorf("refused calls should log nothing, got %+v", sink.events)
	}
	if err := sess.Abandon("i_1", "another build started it"); err != nil {
		t.Fatalf("abandon should end it, got %v", err)
	}
}

// A log without the mark that holds an event only this build writes was
// written by an earlier build of this line, whose moves continue.
func TestAnUnmarkedLogHoldingAForkOnlyEventKeepsItsMoves(t *testing.T) {
	spec := patchSpec(t)
	resolve := func(string) (*engine.Spec, error) { return spec, nil }
	log := append(unmarkedLog(), engine.Event{V: 1, TS: time.Date(2026, 11, 1, 10, 0, 1, 0, time.UTC), Session: "s_other", Seq: 4, Event: engine.EventModel, Data: map[string]any{"model": "a-model"}})
	sess, err := newPatchEngine().ReplaySession("s_other", "tester", log, resolve, &recordingSink{})
	if err != nil {
		t.Fatal(err)
	}

	sv, err := sess.Serve("i_1")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Goal == engine.ForeignMoveGoal || sv.ReportSchema == nil {
		t.Fatalf("a move of an unmarked log holding a fork-only event should serve its step, got %+v", sv)
	}
	if _, err := sess.Report("i_1", map[string]any{"goal": "done"}); err != nil {
		t.Errorf("a report on it should pass, got %v", err)
	}
}

// The first event this build appends to a log holding only stock's events is
// the mark, so what it appends later never makes the stock moves its own.
func TestAStockLogThisBuildAppendsToKeepsItsMovesForeign(t *testing.T) {
	spec := patchSpec(t)
	resolve := func(string) (*engine.Spec, error) { return spec, nil }
	sink := &recordingSink{}
	sess, err := newPatchEngine().ReplaySession("s_other", "tester", unmarkedLog(), resolve, sink)
	if err != nil {
		t.Fatal(err)
	}
	sess.SetLabel("picked up")
	if len(sink.events) != 2 || sink.events[0].Event != engine.EventBuildMark || sink.events[1].Event != engine.EventLabeled {
		t.Fatalf("the first append should log the mark before it, got %+v", sink.events)
	}

	later := engine.Event{V: 1, TS: time.Date(2026, 11, 1, 10, 0, 2, 0, time.UTC), Session: "s_other", Seq: 6, Event: engine.EventModel, Data: map[string]any{"model": "a-model"}}
	replayed, err := newPatchEngine().ReplaySession("s_other", "tester", append(append(unmarkedLog(), sink.events...), later), resolve, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sv, _ := replayed.Serve("i_1"); sv.Goal != engine.ForeignMoveGoal {
		t.Errorf("the stock move should stay abandon-only, got %+v", sv)
	}
}

func TestAMoveStartedAfterTheMarkContinues(t *testing.T) {
	spec := patchSpec(t)
	resolve := func(string) (*engine.Spec, error) { return spec, nil }
	sink := &recordingSink{}
	sess, err := newPatchEngine().ReplaySession("s_other", "tester", unmarkedLog(), resolve, sink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Start(spec, map[string]any{"body": "beta"}, ""); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) < 2 || sink.events[0].Event != engine.EventBuildMark || sink.events[1].Event != engine.EventStarted {
		t.Fatalf("the first move this build starts in an unmarked session should log the mark before it, got %+v", sink.events)
	}

	replayed, err := newPatchEngine().ReplaySession("s_other", "tester", append(unmarkedLog(), sink.events...), resolve, nil)
	if err != nil {
		t.Fatal(err)
	}
	sv, err := replayed.Serve("i_2")
	if err != nil {
		t.Fatal(err)
	}
	if sv.ReportSchema == nil || sv.Goal == engine.ForeignMoveGoal {
		t.Errorf("a move started after the mark should serve its step, got %+v", sv)
	}
	if held, _ := replayed.Serve("i_1"); held.Goal != engine.ForeignMoveGoal {
		t.Errorf("the move started before the mark should stay abandon-only, got %+v", held)
	}
}
