package engine_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// hookMachine has a gate holding on its own findings channel, a gate whose
// transitions read its channel, and an op raising rounds and an announced
// event.
const hookMachine = `state:
    body: {type: text, desc: x}
steps:
    - id: draft
      collect: [body]
      guard: noHighStepFindings
      findings: lint
      transitions:
          - when: hasBody
            to: check
    - id: check
      op: work
      findings: findings
      transitions:
          - when: noHighFindings
            to: pick
          - otherwise: review
    - id: review
      chooser: user
      options:
          - {choice: back, to: draft}
    - id: pick
      chooser: user
      options:
          - {choice: done, to: end(completed)}`

const hookBody = "## unit: draft\n\nDraft.\n"

// scriptedHooks records every point it is fired at and answers per event.
type scriptedHooks struct {
	points   []engine.HookPoint
	findings map[string][]query.Finding
	fail     map[string]error
}

func (h *scriptedHooks) Fire(point engine.HookPoint) ([]query.Finding, error) {
	h.points = append(h.points, point)
	if err := h.fail[point.Event]; err != nil {
		return nil, err
	}
	return h.findings[point.Event], nil
}

func (h *scriptedHooks) events() []string {
	var names []string
	for _, point := range h.points {
		names = append(names, point.Event)
	}
	return names
}

func (h *scriptedHooks) point(t *testing.T, event string) engine.HookPoint {
	t.Helper()
	for _, point := range h.points {
		if point.Event == event {
			return point
		}
	}
	t.Fatalf("no hook fired at %s; fired: %v", event, h.events())
	return engine.HookPoint{}
}

type memorySink struct{ events []engine.Event }

func (m *memorySink) Append(e engine.Event) (uint64, error) {
	m.events = append(m.events, e)
	return uint64(e.Seq), nil
}

var highLint = []query.Finding{{Severity: query.SeverityHigh, Category: "lint", Observation: "passive voice"}}

type hookWorld struct {
	engine *engine.Engine
	spec   *engine.Spec
	hooks  *scriptedHooks
	sink   *memorySink
	runs   int
}

func newHookWorld(t *testing.T) *hookWorld {
	t.Helper()
	w := &hookWorld{hooks: &scriptedHooks{findings: map[string][]query.Finding{}, fail: map[string]error{}}, sink: &memorySink{}}
	registry := engine.NewRegistry()
	if err := registry.RegisterCommand(engine.Command{
		Doc:       engine.FuncDoc{Name: "work", Writes: []string{"findings"}},
		Rounds:    true,
		Announces: "thing.done",
		Fn: func(ctx *engine.Context) error {
			w.runs++
			for round := 1; round <= 2; round++ {
				if err := ctx.Round(map[string]any{"verify": map[string]any{"round": round}}); err != nil {
					return err
				}
			}
			return ctx.Store.WriteEngine("findings", []query.Finding{})
		},
	}); err != nil {
		t.Fatal(err)
	}
	w.engine = engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)})
	w.spec = mustParseLanes(t, hookMachine, hookBody)
	return w
}

func (w *hookWorld) start(t *testing.T) (*engine.Session, *engine.Serve) {
	t.Helper()
	session := w.engine.NewSession("s_hooks", "tester", w.sink, engine.WithHooks(w.hooks))
	serve, err := session.Start(w.spec, nil, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return session, serve
}

func TestHooksFireAtEveryStepEventInOrder(t *testing.T) {
	w := newHookWorld(t)
	session, serve := w.start(t)
	serve, err := session.Report(serve.Instance, map[string]any{"body": "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	if serve.Step != "pick" {
		t.Fatalf("a clean run should reach pick, sits at %s", serve.Step)
	}
	if _, err := session.Answer(serve.Instance, "pick", "done", nil, "done"); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"lanesproc.draft.start", "lanesproc.draft", "lanesproc.check.start",
		"lanesproc.check.round", "lanesproc.check.round", "thing.done",
		"lanesproc.check", "lanesproc.pick.start", "lanesproc.pick",
	}
	if got := w.hooks.events(); !slices.Equal(got, want) {
		t.Fatalf("events fired %v, want %v", got, want)
	}
	done := w.hooks.point(t, "lanesproc.draft")
	if done.Transition != "check" || done.Phase != engine.HookDone || !done.Channel || done.Findings == nil {
		t.Errorf("draft being done should name its transition and carry its findings channel, got %+v", done)
	}
	if arrival := w.hooks.point(t, "lanesproc.pick.start"); arrival.Channel || arrival.Transition != "" {
		t.Errorf("arriving at a step without a channel should carry neither findings nor a transition, got %+v", arrival)
	}
	if round := w.hooks.point(t, "lanesproc.check.round"); round.Phase != engine.HookRound || round.Extra["verify"] == nil {
		t.Errorf("a round should carry the fields its command adds, got %+v", round)
	}
	if end := w.hooks.points[len(w.hooks.points)-1]; end.Transition != engine.EndCompleted {
		t.Errorf("the last step being done should name the end target, got %q", end.Transition)
	}
}

func TestHookHighFindingHoldsAGateGuardedOnItsChannel(t *testing.T) {
	w := newHookWorld(t)
	session, serve := w.start(t)
	w.hooks.findings["lanesproc.draft"] = highLint

	serve, err := session.Report(serve.Instance, map[string]any{"body": "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	if serve.Step != "draft" {
		t.Fatalf("a high hook finding should hold draft, moved to %s", serve.Step)
	}
	if len(serve.Failing) != 1 || serve.Failing[0].Name != "noHighStepFindings" || !strings.Contains(serve.Failing[0].Message, "passive voice") {
		t.Fatalf("the hold should name the hook's finding, got %+v", serve.Failing)
	}

	delete(w.hooks.findings, "lanesproc.draft")
	serve, err = session.Report(serve.Instance, map[string]any{"body": "a revised draft"})
	if err != nil {
		t.Fatal(err)
	}
	if serve.Step != "pick" {
		t.Fatalf("a revision the hooks pass should leave draft, sits at %s (failing %+v)", serve.Step, serve.Failing)
	}
}

func TestHookHighFindingRedirectsATransitionReadingItsChannel(t *testing.T) {
	w := newHookWorld(t)
	session, serve := w.start(t)
	w.hooks.findings["lanesproc.check"] = highLint

	serve, err := session.Report(serve.Instance, map[string]any{"body": "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	if serve.Step != "review" {
		t.Fatalf("a high finding joined into check's channel should route to review, sits at %s", serve.Step)
	}
	if done := w.hooks.point(t, "lanesproc.check"); done.Transition != "pick" {
		t.Errorf("the done event should name the transition selected before the hooks ran, got %q", done.Transition)
	}

}

func TestHookJoinIsDroppedOnALaterArrival(t *testing.T) {
	mediumLint := []query.Finding{{Severity: query.SeverityMedium, Category: "lint", Observation: "long sentence"}}
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "replayed"}[replay], func(t *testing.T) {
			w := newHookWorld(t)
			session, serve := w.start(t)
			w.hooks.findings["lanesproc.draft"] = mediumLint
			w.hooks.findings["lanesproc.check"] = highLint
			if _, err := session.Report(serve.Instance, map[string]any{"body": "a draft"}); err != nil {
				t.Fatal(err)
			}
			w.hooks.findings = map[string][]query.Finding{}
			if replay {
				resolve := func(string) (*engine.Spec, error) { return w.spec, nil }
				var err error
				session, err = w.engine.ReplaySession("s_hooks", "tester", w.sink.events, resolve, w.sink, engine.WithHooks(w.hooks))
				if err != nil {
					t.Fatal(err)
				}
			}
			fired := len(w.hooks.points)
			serve, err := session.Answer(serve.Instance, "review", "back", nil, "back")
			if err != nil {
				t.Fatal(err)
			}
			if serve.Step != "pick" {
				t.Fatalf("the run the hooks pass should reach pick, sits at %s", serve.Step)
			}
			for _, point := range w.hooks.points[fired:] {
				if point.Event == "lanesproc.draft" && len(point.Findings) > 0 {
					t.Errorf("a later arrival should judge without the findings joined on an earlier one, got %+v", point.Findings)
				}
			}
		})
	}
}

func TestHookFindingsOffADoneChannelAreRefused(t *testing.T) {
	w := newHookWorld(t)
	w.hooks.findings["lanesproc.draft.start"] = highLint
	session := w.engine.NewSession("s_hooks", "tester", w.sink, engine.WithHooks(w.hooks))
	_, err := session.Start(w.spec, nil, "")
	if err == nil || !strings.Contains(err.Error(), "only a step with a findings channel being done accepts them") {
		t.Fatalf("findings on arriving at a step should fail the call, got %v", err)
	}
}

func TestHookFailureFailsTheCall(t *testing.T) {
	w := newHookWorld(t)
	session, serve := w.start(t)
	w.hooks.fail["lanesproc.check.start"] = errors.New("exit status 3")

	_, err := session.Report(serve.Instance, map[string]any{"body": "a draft"})
	if err == nil || !strings.Contains(err.Error(), "hooks at lanesproc.check.start: exit status 3") {
		t.Fatalf("a failing hook should fail the call naming its event, got %v", err)
	}
}

func TestHookFailingOnAnAnnouncedEventKeepsTheOpDone(t *testing.T) {
	w := newHookWorld(t)
	session, serve := w.start(t)
	w.hooks.fail["thing.done"] = errors.New("exit status 1")
	if _, err := session.Report(serve.Instance, map[string]any{"body": "a draft"}); err == nil {
		t.Fatal("a failing hook on the announced event should fail the call")
	}

	delete(w.hooks.fail, "thing.done")
	serve, err := session.Report(serve.Instance, map[string]any{"body": "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	if w.runs != 1 {
		t.Errorf("the committed op should not run again after its announcement failed, ran %d times", w.runs)
	}
	if serve.Step != "pick" {
		t.Errorf("the next call should continue past the op, sits at %s", serve.Step)
	}
}

func TestHookHoldSurvivesReplay(t *testing.T) {
	w := newHookWorld(t)
	session, serve := w.start(t)
	w.hooks.findings["lanesproc.draft"] = highLint
	if _, err := session.Report(serve.Instance, map[string]any{"body": "a draft"}); err != nil {
		t.Fatal(err)
	}

	delete(w.hooks.findings, "lanesproc.draft")
	resolve := func(string) (*engine.Spec, error) { return w.spec, nil }
	replayed, err := w.engine.ReplaySession("s_hooks", "tester", w.sink.events, resolve, w.sink, engine.WithHooks(w.hooks))
	if err != nil {
		t.Fatal(err)
	}
	held, err := replayed.Serve(serve.Instance)
	if err != nil {
		t.Fatal(err)
	}
	if held.Step != "draft" || len(held.Failing) == 0 {
		t.Fatalf("a replayed hold should still hold draft, got step %s failing %+v", held.Step, held.Failing)
	}
	next, err := replayed.Report(serve.Instance, map[string]any{"body": "a revised draft"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Step != "pick" {
		t.Fatalf("after replay the next attempt should drop the old hook findings and leave draft, sits at %s (failing %+v)", next.Step, next.Failing)
	}
}

func TestHookEventsListWhatTheSpecsRaise(t *testing.T) {
	w := newHookWorld(t)
	events := engine.HookEvents([]*engine.Spec{w.spec}, w.engine.Registry)
	byName := map[string]bool{}
	var names []string
	for _, event := range events {
		names = append(names, event.Name)
		byName[event.Name] = event.Findings
	}
	if !slices.IsSorted(names) {
		t.Errorf("events should come sorted by name, got %v", names)
	}
	for _, name := range []string{"lanesproc.draft.start", "lanesproc.review", "lanesproc.check.round", "thing.done"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("events should list %s, got %v", name, names)
		}
	}
	if _, ok := byName["lanesproc.draft.round"]; ok {
		t.Error("a step whose commands declare no rounds should raise no round event")
	}
	if !byName["lanesproc.draft"] || !byName["lanesproc.check"] {
		t.Error("a step with a findings channel being done should accept findings")
	}
	if byName["lanesproc.draft.start"] || byName["lanesproc.pick"] || byName["thing.done"] {
		t.Error("only a step with a findings channel being done should accept findings")
	}
}

func TestSpecFindingsChannelIsAGateOnEngineWrittenField(t *testing.T) {
	requireProblem(t, parseFails(t, `steps:
    - id: ask
      chooser: user
      findings: lint
      options:
          - {choice: done, to: end(completed)}`, ""), "findings applies only to a gate")
	requireProblem(t, parseFails(t, `state:
    lint: {type: text, desc: x}
steps:
    - id: draft
      findings: lint
      transitions:
          - otherwise: end(completed)`, ""), "a findings channel is engine-written")
	requireProblem(t, parseFails(t, `steps:
    - id: draft
      findings: "not a field"
      transitions:
          - otherwise: end(completed)`, ""), "not a field name")
}

// A failing start hook holds its step: every later call raises the event
// again, and the step's op runs only once the hook passes
// (20261109-213250-d-tac-bve).
func TestHookFailingAtStartHoldsTheStepUntilItPasses(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "replayed"}[replay], func(t *testing.T) {
			w := newHookWorld(t)
			session, serve := w.start(t)
			w.hooks.fail["lanesproc.check.start"] = errors.New("exit status 2")

			for attempt := 1; attempt <= 2; attempt++ {
				if replay {
					resolve := func(string) (*engine.Spec, error) { return w.spec, nil }
					var err error
					session, err = w.engine.ReplaySession("s_hooks", "tester", w.sink.events, resolve, w.sink, engine.WithHooks(w.hooks))
					if err != nil {
						t.Fatal(err)
					}
				}
				_, err := session.Report(serve.Instance, map[string]any{"body": "a draft"})
				if err == nil || !strings.Contains(err.Error(), "step check is held until its start hooks pass") {
					t.Fatalf("attempt %d: a failing start hook should fail the call naming the held step, got %v", attempt, err)
				}
				if w.runs != 0 {
					t.Fatalf("attempt %d: the held step's op ran %d times", attempt, w.runs)
				}
			}
			if fired := countEvents(w.hooks.events(), "lanesproc.check.start"); fired != 2 {
				t.Errorf("the start hook should fire again on the retry, fired %d times", fired)
			}

			delete(w.hooks.fail, "lanesproc.check.start")
			next, err := session.Report(serve.Instance, map[string]any{"body": "a draft"})
			if err != nil {
				t.Fatal(err)
			}
			if w.runs != 1 || next.Step != "pick" {
				t.Fatalf("once the start hook passes the op should run once and the step move on, ran %d times, sits at %s", w.runs, next.Step)
			}
			if fired := countEvents(w.hooks.events(), "lanesproc.check.start"); fired != 3 {
				t.Errorf("the passing start hook should fire once more, fired %d times", fired)
			}
		})
	}
}

func TestHookFailingAtStartHoldsAChooserAnswer(t *testing.T) {
	w := newHookWorld(t)
	session, serve := w.start(t)
	w.hooks.fail["lanesproc.pick.start"] = errors.New("exit status 2")
	if _, err := session.Report(serve.Instance, map[string]any{"body": "a draft"}); err == nil {
		t.Fatal("a failing start hook should fail the call")
	}
	if _, err := session.Answer(serve.Instance, "pick", "done", nil, "done"); err == nil || !strings.Contains(err.Error(), "step pick is held") {
		t.Fatalf("an answer at a held step should fail naming the hold, got %v", err)
	}

	delete(w.hooks.fail, "lanesproc.pick.start")
	done, err := session.Answer(serve.Instance, "pick", "done", nil, "done")
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != engine.StatusCompleted {
		t.Errorf("the answer should complete the run once the start hook passes, status %s", done.Status)
	}
}

func countEvents(events []string, name string) int {
	n := 0
	for _, event := range events {
		if event == name {
			n++
		}
	}
	return n
}
