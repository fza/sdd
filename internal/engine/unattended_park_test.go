package engine_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// parkMachine holds a step an autonomous session parks at on arrival for a
// person, its reason the failure message of the predicate it names.
const parkMachine = `steps:
    - id: hold
      chooser: user
      unattended: {park: personRequired}
      options:
          - {choice: adjust, to: end(completed)}`

func parkSession(t *testing.T, sink engine.EventSink, opts ...engine.SessionOption) (*engine.Session, *engine.Serve) {
	t.Helper()
	spec := mustParseLanes(t, parkMachine, "## unit: hold\n\nAsk the user.\n")
	registry := engine.NewRegistry()
	err := registry.RegisterPredicate(engine.Predicate{
		Doc:         engine.FuncDoc{Name: "personRequired", Doc: "Fails while the draft needs a person."},
		Fn:          func(*engine.Context) (bool, error) { return false, nil },
		FailMessage: "generic failure",
		FailDetail:  func(*engine.Context) string { return "agent limit: a conceptual decision takes a person" },
	})
	if err != nil {
		t.Fatal(err)
	}
	session := engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_park", "tester", sink, opts...)
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return session, sv
}

func TestUnattendedParkOnArrival(t *testing.T) {
	sink := &recordingSink{}
	session, sv := parkSession(t, sink, autonomous())
	if sv.Parked == nil || sv.Parked.Reason != "agent limit: a conceptual decision takes a person" {
		t.Fatalf("an autonomous session should park at the step with the predicate's message as reason, got %+v", sv.Parked)
	}
	if !strings.Contains(sv.Instructions, "Parked at hold: agent limit: a conceptual decision takes a person") {
		t.Errorf("the parked serve should name the rule, got %q", sv.Instructions)
	}
	parked := 0
	for _, ev := range sink.events {
		if ev.Event == engine.EventParked {
			parked++
		}
	}
	if parked != 1 {
		t.Errorf("the park should be logged once, got %d", parked)
	}
	if _, err := session.AnswerWith(sv.Instance, "hold", "adjust", nil, engine.AnswerWords{Reason: "I may"}); err == nil || !strings.Contains(err.Error(), "is parked") {
		t.Errorf("the agent should not answer a parked step, got %v", err)
	}
	if err := session.RefuseUnattendedAbandon(sv.Instance); err == nil {
		t.Error("the agent should not abandon a parked move")
	}

	session.SetDialogue(model.DialogueGuided, "")
	sv, err := session.Serve(sv.Instance)
	if err != nil {
		t.Fatal(err)
	}
	if sv.Parked != nil {
		t.Fatalf("a guided session should serve the step to the person, got parked %+v", sv.Parked)
	}
	if _, err := session.AnswerWith(sv.Instance, "hold", "adjust", nil, engine.AnswerWords{User: "adjust it"}); err != nil {
		t.Errorf("the person should answer the step after resuming guided: %v", err)
	}
}

func TestUnattendedParkLeavesAGuidedSessionAlone(t *testing.T) {
	_, sv := parkSession(t, nopSink{})
	if sv.Parked != nil {
		t.Errorf("a guided session should never park, got %+v", sv.Parked)
	}
}

func TestUnattendedParkSpecProblems(t *testing.T) {
	cases := []struct{ name, step, want string }{
		{name: "with answer", step: "chooser: user\n      unattended: {park: personRequired, answer: go}", want: "unattended park excludes answer and limit"},
		{name: "with limit", step: "chooser: user\n      unattended: {park: personRequired, limit: 1}", want: "unattended park excludes answer and limit"},
		{name: "not a name", step: "chooser: user\n      unattended: {park: \"a b\"}", want: `unattended park names "a b"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machine := "steps:\n    - id: pick\n      " + tc.step + "\n      options:\n          - {choice: go, to: end(completed)}"
			requireProblem(t, parseFails(t, machine, "## unit: pick\n\nPick.\n"), tc.want)
		})
	}
}
