package engine_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/query"
)

// stageMachine runs two steps as one hook step, verify: an op writing its
// verdict and then raising a round, and a gate holding on the stage's findings
// channel.
const stageMachine = `state:
    body: {type: text, desc: x}
steps:
    - id: draft
      collect: [body]
      transitions:
          - when: hasBody
            to: first
    - id: first
      stage: verify
      op: judge
      transitions:
          - otherwise: second
    - id: second
      stage: verify
      guard: noHighStepFindings
      findings: verifyLint
      transitions:
          - otherwise: pick
    - id: pick
      chooser: user
      options:
          - {choice: done, to: end(completed)}`

func newStageWorld(t *testing.T) *hookWorld {
	t.Helper()
	w := newHookWorld(t)
	if err := w.engine.Registry.RegisterCommand(engine.Command{
		Doc:    engine.FuncDoc{Name: "judge", Writes: []string{"verdict"}},
		Rounds: true,
		Fn: func(ctx *engine.Context) error {
			if err := ctx.Store.WriteEngine("verdict", "held"); err != nil {
				return err
			}
			return ctx.Round(nil)
		},
	}); err != nil {
		t.Fatal(err)
	}
	w.spec = mustParseLanes(t, stageMachine, hookBody)
	return w
}

func TestHookStageRaisesOneStepsEvents(t *testing.T) {
	w := newStageWorld(t)
	session, serve := w.start(t)
	serve, err := session.Report(serve.Instance, map[string]any{"body": "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	if serve.Step != "pick" {
		t.Fatalf("a clean run should pass the stage, sits at %s", serve.Step)
	}
	want := []string{
		"lanesproc.draft.start", "lanesproc.draft", "lanesproc.verify.start",
		"lanesproc.verify.round", "lanesproc.verify", "lanesproc.pick.start",
	}
	if got := w.hooks.events(); !slices.Equal(got, want) {
		t.Fatalf("the stage's steps should raise one step's events, fired %v", got)
	}
	if round := w.hooks.point(t, "lanesproc.verify.round"); round.Step != "verify" || round.Phase != engine.HookRound {
		t.Errorf("a round inside the stage should name the stage, got %+v", round)
	}
	left := w.hooks.point(t, "lanesproc.verify")
	if left.Step != "verify" || left.Transition != "pick" || !left.Channel {
		t.Errorf("leaving the stage should name the stage, the target and carry its channel, got %+v", left)
	}
	if entered := w.hooks.point(t, "lanesproc.verify.start"); entered.Step != "verify" || entered.Channel {
		t.Errorf("entering the stage at a step without the channel should name the stage and carry no findings, got %+v", entered)
	}
}

// verdictAtRound records the verdict a round's store holds while the hooks
// run, before the command's writes are committed.
type verdictAtRound struct {
	*scriptedHooks
	verdict any
}

func (h *verdictAtRound) Fire(point engine.HookPoint) ([]query.Finding, error) {
	if point.Phase == engine.HookRound {
		h.verdict, _ = point.Store.Get("verdict")
	}
	return h.scriptedHooks.Fire(point)
}

func TestHookRoundReadsWhatItsCommandWroteBefore(t *testing.T) {
	w := newStageWorld(t)
	hooks := &verdictAtRound{scriptedHooks: w.hooks}
	session := w.engine.NewSession("s_hooks", "tester", w.sink, engine.WithHooks(hooks))
	serve, err := session.Start(w.spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Report(serve.Instance, map[string]any{"body": "a draft"}); err != nil {
		t.Fatal(err)
	}
	if hooks.verdict != "held" {
		t.Errorf("a round's payload should read the writes its command made before raising it, read %v", hooks.verdict)
	}
}

func TestHookStageHighFindingHoldsItsChannelStep(t *testing.T) {
	w := newStageWorld(t)
	session, serve := w.start(t)
	w.hooks.findings["lanesproc.verify"] = highLint
	serve, err := session.Report(serve.Instance, map[string]any{"body": "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	if serve.Step != "second" {
		t.Fatalf("a high finding on leaving the stage should hold its channel step, moved to %s", serve.Step)
	}
	if len(serve.Failing) != 1 || !strings.Contains(serve.Failing[0].Message, "passive voice") {
		t.Fatalf("the hold should name the hook's finding, got %+v", serve.Failing)
	}
}

func TestHookEventsNameTheStage(t *testing.T) {
	w := newStageWorld(t)
	byName := map[string]bool{}
	for _, event := range engine.HookEvents([]*engine.Spec{w.spec}, w.engine.Registry) {
		byName[event.Name] = event.Findings
	}
	if !byName["lanesproc.verify"] {
		t.Error("leaving a stage holding a findings channel should accept findings")
	}
	if _, ok := byName["lanesproc.verify.round"]; !ok {
		t.Error("a stage whose commands declare rounds should raise its round event")
	}
	for _, name := range []string{"lanesproc.first.start", "lanesproc.second", "lanesproc.first.round"} {
		if _, ok := byName[name]; ok {
			t.Errorf("a step inside a stage should raise no event of its own, listed %s", name)
		}
	}
}

func TestSpecStageNamesOneHookStep(t *testing.T) {
	requireProblem(t, parseFails(t, `steps:
    - id: draft
      stage: pick
      transitions:
          - otherwise: pick
    - id: pick
      transitions:
          - otherwise: end(completed)`, ""), `stage "pick" is also a step id`)
	requireProblem(t, parseFails(t, `steps:
    - id: first
      stage: verify
      findings: lintA
      transitions:
          - otherwise: second
    - id: second
      stage: verify
      findings: lintB
      transitions:
          - otherwise: end(completed)`, ""), `stage "verify" already has its findings channel on step first`)
	requireProblem(t, parseFails(t, `steps:
    - id: first
      stage: "not a name"
      transitions:
          - otherwise: end(completed)`, ""), "is not a step name")
}
