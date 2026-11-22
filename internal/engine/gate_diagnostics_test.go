package engine_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// selectorMachine holds at draft once the body is in: the quick path is
// chosen by the fast selector, the done path is blocked by a negated clause.
const selectorMachine = `state:
    body: {type: text, desc: x}
steps:
    - id: draft
      collect: [body]
      transitions:
          - when: hasBody and fast and ready
            to: quick
          - when: hasBody and not waived and not blocked
            to: end(completed)
    - id: quick
      chooser: user
      options:
          - {choice: done, to: end(completed)}`

// guardedMachine holds at draft on a step guard the fast selector alone can
// fail.
const guardedMachine = `state:
    body: {type: text, desc: x}
steps:
    - id: draft
      collect: [body]
      guard: %s
      transitions:
          - when: hasBody
            to: end(completed)`

type gateWorld struct {
	values map[string]bool
	engine *engine.Engine
}

func newGateWorld(t *testing.T) *gateWorld {
	t.Helper()
	w := &gateWorld{values: map[string]bool{"blocked": true}}
	registry := engine.NewRegistry()
	for _, p := range []struct {
		name, fail string
		selector   bool
	}{
		{name: "fast", selector: true},
		{name: "ready", fail: "ready is owed"},
		{name: "waived", fail: "waived is owed"},
		{name: "blocked", fail: "blocked is owed"},
		{name: "confirmed", fail: "confirmed is owed"},
	} {
		if err := registry.RegisterPredicate(engine.Predicate{
			Doc:         engine.FuncDoc{Name: p.name},
			Fn:          func(*engine.Context) (bool, error) { return w.values[p.name], nil },
			FailMessage: p.fail,
			Selector:    p.selector,
		}); err != nil {
			t.Fatal(err)
		}
	}
	w.engine = engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)})
	return w
}

func (w *gateWorld) held(t *testing.T, machine string) *engine.Serve {
	t.Helper()
	session := w.engine.NewSession("s_gate", "tester", &memorySink{})
	serve, err := session.Start(mustParseLanes(t, machine, "## unit: draft\n\nDraft.\n"), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	serve, err = session.Report(serve.Instance, map[string]any{"body": "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	if serve.Step != "draft" {
		t.Fatalf("the gate should hold draft, moved to %s", serve.Step)
	}
	return serve
}

func TestGateDiagnosticsSkipATransitionASelectorRulesOut(t *testing.T) {
	w := newGateWorld(t)
	serve := w.held(t, selectorMachine)
	if want := []string{"predicate blocked holds"}; !slices.Equal(serve.Diagnostics, want) {
		t.Fatalf("the quick path is off while fast is false, so only the negated block is owed: got %v, want %v", serve.Diagnostics, want)
	}

	w.values["fast"] = true
	serve = w.held(t, selectorMachine)
	if want := []string{"ready is owed", "predicate blocked holds"}; !slices.Equal(serve.Diagnostics, want) {
		t.Fatalf("with fast true the quick path is on and owes ready: got %v, want %v", serve.Diagnostics, want)
	}
}

func TestGateDiagnosticsListANegatedClauseOnlyWhenItBlocks(t *testing.T) {
	w := newGateWorld(t)
	w.values["blocked"] = false
	w.values["waived"] = true
	serve := w.held(t, selectorMachine)
	if want := []string{"predicate waived holds"}; !slices.Equal(serve.Diagnostics, want) {
		t.Fatalf("a false predicate under not satisfies its clause, a true one blocks: got %v, want %v", serve.Diagnostics, want)
	}
}

func TestGateDiagnosticsOfAStepGuard(t *testing.T) {
	w := newGateWorld(t)
	serve := w.held(t, fmt.Sprintf(guardedMachine, "confirmed or fast"))
	if want := []string{"confirmed is owed"}; !slices.Equal(serve.Diagnostics, want) {
		t.Fatalf("a selector disjunct is never owed work: got %v, want %v", serve.Diagnostics, want)
	}

	serve = w.held(t, fmt.Sprintf(guardedMachine, "fast"))
	if want := []string{"predicate fast does not hold"}; !slices.Equal(serve.Diagnostics, want) {
		t.Fatalf("a step guard ruled out by a selector still names it: got %v, want %v", serve.Diagnostics, want)
	}
}

func TestGuardBlocking(t *testing.T) {
	values := map[string]bool{"on": true}
	selectors := map[string]bool{"mode": true, "other": true}
	eval := func(name string) (bool, error) {
		if name == "boom" {
			return false, errors.New("predicate exploded")
		}
		return values[name], nil
	}
	isSelector := func(name string) bool { return selectors[name] }

	tests := []struct {
		src      string
		holds    bool
		ruledOut bool
		clauses  []engine.Clause
	}{
		{src: "on and not off", holds: true},
		{src: "work and not on", clauses: []engine.Clause{{Name: "work"}, {Name: "on", Negated: true}}},
		{src: "not (on and work)", holds: true},
		{src: "not (on or work)", clauses: []engine.Clause{{Name: "on", Negated: true}}},
		{src: "mode and work", ruledOut: true},
		{src: "(mode and work) or chore", clauses: []engine.Clause{{Name: "chore"}}},
		{src: "mode or other", ruledOut: true},
		{src: "work or work", clauses: []engine.Clause{{Name: "work"}}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			g, err := engine.ParseGuard(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			holds, ruledOut, clauses, err := g.Blocking(eval, isSelector)
			if err != nil {
				t.Fatal(err)
			}
			if holds != tt.holds || ruledOut != tt.ruledOut || !slices.Equal(clauses, tt.clauses) {
				t.Fatalf("Blocking = %v %v %v, want %v %v %v", holds, ruledOut, clauses, tt.holds, tt.ruledOut, tt.clauses)
			}
		})
	}

	g, err := engine.ParseGuard("mode and work")
	if err != nil {
		t.Fatal(err)
	}
	_, ruledOut, clauses, err := g.Blocking(eval, nil)
	if err != nil || ruledOut || !slices.Equal(clauses, []engine.Clause{{Name: "mode"}, {Name: "work"}}) {
		t.Fatalf("without selectors every blocking clause is named, got %v %v %v", ruledOut, clauses, err)
	}

	g, err = engine.ParseGuard("on or boom")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := g.Blocking(eval, isSelector); err == nil {
		t.Fatal("a predicate error must propagate even where the other disjunct holds")
	}
}
