package engine_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// The dispatcher answers the delegate's findings or sends it a revision,
// which its own command writes; the task waits for either and routes on the
// one that came.
const revisingDispatcherMachine = `state:
    goal: {type: text, desc: what the review is for}
    synthesis: {type: text, desc: the delegate's findings}
    brief: {type: text, desc: the answers to the findings}
steps:
    - id: ask
      chooser: agent
      options:
          - {choice: delegate, collect: [goal], dispatch: {procedure: revisetask, seed: {topic: goal}, receive: {synthesis: plan}, send: {contract: brief, revision: note}}, to: wait}
    - id: wait
      await: [synthesis]
      transitions:
          - otherwise: answer
    - id: answer
      chooser: agent
      options:
          - {choice: reply, collect: [brief], to: wait}
          - {choice: revise, to: revise}
    - id: revise
      op: writeNote
      transitions:
          - otherwise: wait`

const reviseTaskMachine = `rounds: 3
state:
    topic: {type: text, desc: what to review}
    plan: {type: text, desc: the findings}
    contract: {type: text, desc: the dispatching side's answers}
    revision: {type: text, desc: the dispatching side's revision}
steps:
    - id: review
      collect: [plan]
      deliver: [plan]
      transitions:
          - when: hasPlan
            to: discuss
    - id: discuss
      awaitAny: [contract, revision]
      transitions:
          - when: revisionArrived
            to: rereview
          - otherwise: respond
    - id: rereview
      chooser: agent
      options:
          - {choice: judge, collect: [plan], to: review}
    - id: respond
      chooser: agent
      options:
          - {choice: again, collect: [plan], to: review}`

func revisingRegistry(t *testing.T) *engine.Registry {
	t.Helper()
	registry := engine.NewRegistry()
	err := registry.RegisterCommand(engine.Command{
		Doc: engine.FuncDoc{Name: "writeNote", Doc: "Writes the revision the dispatcher sends.", Writes: []string{"note"}},
		Fn:  func(ctx *engine.Context) error { return ctx.Store.WriteEngine("note", "the draft changed") },
	})
	if err != nil {
		t.Fatal(err)
	}
	err = registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{Name: "revisionArrived", Doc: "The gate holds a revision."},
		Fn:  func(ctx *engine.Context) (bool, error) { return slices.Contains(ctx.Arrived, "revision"), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestAwaitAnyPassesOnEitherDeliveryAndNamesTheOneThatCame(t *testing.T) {
	registry := revisingRegistry(t)
	parentSpec, err := engine.LoadSpec(delegateEntry(t, "dispatcher", "move", revisingDispatcherMachine), registry)
	if err != nil {
		t.Fatal(err)
	}
	childSpec, err := engine.LoadSpec(delegateEntry(t, "revisetask", "task", reviseTaskMachine), registry)
	if err != nil {
		t.Fatal(err)
	}
	courier := &sessionCourier{sessions: map[string]*engine.Session{}}
	eng := engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)})
	parent := eng.NewSession("s_parent", "tester", &recordingSink{}, engine.WithCourier(courier))
	courier.sessions["s_parent"] = parent
	sv, err := parent.Start(parentSpec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Answer(sv.Instance, "ask", "delegate", map[string]any{"goal": "check the draft"}, ""); err != nil {
		t.Fatal(err)
	}
	child := eng.NewSession("s_child", "tester", &recordingSink{}, engine.WithLineage(engine.Link{Session: "s_parent", Instance: sv.Instance}), engine.WithCourier(courier))
	courier.sessions["s_child"] = child
	childInst := child.NextInstanceID()
	seed, err := parent.Fork(sv.Instance, engine.Link{Session: "s_child", Instance: childInst}, childSpec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := child.Start(childSpec, seed, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Report(childInst, map[string]any{"plan": "f1"}); err != nil {
		t.Fatal(err)
	}
	held, err := child.Poll(childInst)
	if err != nil {
		t.Fatal(err)
	}
	if held.Step != "discuss" || !slices.ContainsFunc(held.Diagnostics, func(d string) bool {
		return strings.Contains(d, "waiting for the dispatching move to send contract or revision")
	}) {
		t.Fatalf("the gate should hold naming either field, got %s %v", held.Step, held.Diagnostics)
	}

	if _, err := parent.Poll(sv.Instance); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Answer(sv.Instance, "answer", "revise", nil, ""); err != nil {
		t.Fatal(err)
	}
	next, err := child.Poll(childInst)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := child.Instance(childInst)
	if revision, _ := inst.Store.Get("revision"); next.Step != "rereview" || revision != "the draft changed" {
		t.Fatalf("the value the dispatcher's command wrote should reach the child and route it, at %s holding %v", next.Step, revision)
	}

	if _, err := child.Answer(childInst, "rereview", "judge", map[string]any{"plan": "f2"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Poll(sv.Instance); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Answer(sv.Instance, "answer", "reply", map[string]any{"brief": "a1"}, ""); err != nil {
		t.Fatal(err)
	}
	next, err = child.Poll(childInst)
	if err != nil {
		t.Fatal(err)
	}
	if next.Step != "respond" {
		t.Fatalf("passing the gate should consume the revision, so the answer routes to respond, at %s", next.Step)
	}
}

func TestDispatchSendsADeclaredOrWrittenField(t *testing.T) {
	machine := strings.Replace(revisingDispatcherMachine, "revision: note}", "revision: nothing}", 1)
	_, err := engine.LoadSpec(delegateEntry(t, "dispatcher", "move", machine), revisingRegistry(t))
	if err == nil || !strings.Contains(err.Error(), `sends "revision" from "nothing", which is no declared param or state field of this procedure and no command of it writes`) {
		t.Fatalf("a send from a field nothing declares or writes should be refused, got %v", err)
	}
}
