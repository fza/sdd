package engine_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// dialogueMachine holds a delegable user chooser followed by one that stays
// the user's.
const dialogueMachine = `steps:
    - id: pick
      chooser: user
      delegable: true
      options:
          - {choice: go, to: keep}
    - id: keep
      chooser: user
      options:
          - {choice: done, to: end(completed)}`

const dialogueBody = "## unit: pick\n\n{{if eq .dialogueMode \"autonomous\"}}Decide yourself.{{else}}Ask the user.{{end}}\n\n## unit: keep\n\nAsk the user.\n"

func dialogueSession(t *testing.T, sink engine.EventSink, opts ...engine.SessionOption) (*engine.Session, *engine.Serve) {
	t.Helper()
	spec := mustParseLanes(t, dialogueMachine, dialogueBody)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	session := eng.NewSession("s_dialogue", "tester", sink, opts...)
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return session, sv
}

func autonomous() engine.SessionOption {
	return engine.WithDialogue(model.DialogueAutonomous, "config.local.yaml")
}

func schemaRequires(sv *engine.Serve, field string) bool {
	required, _ := sv.ReportSchema["required"].([]string)
	return slices.Contains(required, field)
}

func schemaHas(sv *engine.Serve, field string) bool {
	props, _ := sv.ReportSchema["properties"].(map[string]any)
	_, ok := props[field]
	return ok
}

func TestDelegableChooserServe(t *testing.T) {
	cases := []struct {
		name       string
		opts       []engine.SessionOption
		wantText   string
		wantGoal   string
		wantReason bool
	}{
		{name: "guided", wantText: "Ask the user.", wantGoal: "put the choice to the user"},
		{name: "autonomous", opts: []engine.SessionOption{autonomous()}, wantText: "Decide yourself.", wantGoal: "decide the choice yourself", wantReason: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, sv := dialogueSession(t, nopSink{}, tc.opts...)
			if sv.UnitText != tc.wantText {
				t.Errorf("the unit should render for the session's mode, got %q", sv.UnitText)
			}
			if !strings.HasPrefix(sv.Goal, tc.wantGoal) {
				t.Errorf("the goal should say who decides, got %q", sv.Goal)
			}
			if schemaHas(sv, "reason") != tc.wantReason {
				t.Errorf("only an autonomous session should offer a reason, got %v", sv.ReportSchema)
			}
			if schemaRequires(sv, "userWords") == tc.wantReason {
				t.Errorf("the user's words should be required exactly where no reason is offered, got %v", sv.ReportSchema)
			}
		})
	}
}

func TestReservedChooserServeInAnAutonomousSession(t *testing.T) {
	session, sv := dialogueSession(t, nopSink{}, autonomous())
	sv, err := session.AnswerWith(sv.Instance, "pick", "go", nil, engine.AnswerWords{Reason: "the only way on"})
	if err != nil {
		t.Fatalf("the agent should take a delegable choice: %v", err)
	}
	if !strings.HasPrefix(sv.Goal, "put the choice to the user") || schemaHas(sv, "reason") || !schemaRequires(sv, "userWords") {
		t.Errorf("an unmarked user chooser should stay the user's, got goal %q schema %v", sv.Goal, sv.ReportSchema)
	}
}

func TestDialogueAnswers(t *testing.T) {
	cases := []struct {
		name        string
		opts        []engine.SessionOption
		chooser     string
		words       engine.AnswerWords
		refusal     string
		wantByAgent string
	}{
		{name: "guided session relays the user's words", chooser: "pick", words: engine.AnswerWords{User: "go on"}},
		{name: "guided session refuses a reason", chooser: "pick", words: engine.AnswerWords{Reason: "obvious"}, refusal: "guided session"},
		{name: "autonomous agent decides a delegable choice", opts: []engine.SessionOption{autonomous()}, chooser: "pick", words: engine.AnswerWords{Reason: "obvious"}, wantByAgent: "agent"},
		{name: "autonomous session relays the user's words", opts: []engine.SessionOption{autonomous()}, chooser: "pick", words: engine.AnswerWords{User: "go on"}, wantByAgent: "user"},
		{name: "autonomous session refuses both", opts: []engine.SessionOption{autonomous()}, chooser: "pick", words: engine.AnswerWords{User: "go on", Reason: "obvious"}, refusal: "not both"},
		{name: "autonomous session refuses neither", opts: []engine.SessionOption{autonomous()}, chooser: "pick", refusal: "needs the user's words or your reason"},
		{name: "autonomous session keeps a reserved choice", opts: []engine.SessionOption{autonomous()}, chooser: "keep", words: engine.AnswerWords{Reason: "obvious"}, refusal: "stays the user's choice"},
		{name: "autonomous session needs words at a reserved choice", opts: []engine.SessionOption{autonomous()}, chooser: "keep", refusal: "stays the user's choice"},
		{name: "autonomous session relays words at a reserved choice", opts: []engine.SessionOption{autonomous()}, chooser: "keep", words: engine.AnswerWords{User: "done"}, wantByAgent: "user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			session, sv := dialogueSession(t, sink, tc.opts...)
			if tc.chooser == "keep" {
				words := engine.AnswerWords{User: "go on"}
				if session.DialogueMode == model.DialogueAutonomous {
					words = engine.AnswerWords{Reason: "the only way on"}
				}
				var err error
				sv, err = session.AnswerWith(sv.Instance, "pick", "go", nil, words)
				if err != nil {
					t.Fatalf("reaching the reserved chooser: %v", err)
				}
			}
			choice := map[string]string{"pick": "go", "keep": "done"}[tc.chooser]
			_, err := session.AnswerWith(sv.Instance, tc.chooser, choice, nil, tc.words)
			if tc.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refusal) {
					t.Fatalf("the answer should be refused saying who must answer, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("the answer should be accepted: %v", err)
			}
			last := sink.events[len(sink.events)-1]
			for _, ev := range slices.Backward(sink.events) {
				if ev.Event == engine.EventChooserAnswer {
					last = ev
					break
				}
			}
			if got, _ := last.Data["answeredBy"].(string); got != tc.wantByAgent {
				t.Errorf("the answer event should record who answered only in an autonomous session, got %q", got)
			}
			if got, _ := last.Data["reason"].(string); got != tc.words.Reason {
				t.Errorf("the answer event should carry the agent's reason, got %q", got)
			}
		})
	}
}

func TestDialogueModeSurvivesReplay(t *testing.T) {
	sink := &recordingSink{}
	dialogueSession(t, sink, autonomous())
	meta := sink.events[0]
	if meta.Event != engine.EventSessionMeta || meta.Data["dialogue"] != "autonomous" || meta.Data["dialogueSource"] != "config.local.yaml" {
		t.Fatalf("the session header should record the mode and the layer that set it, got %+v", meta)
	}
	spec := mustParseLanes(t, dialogueMachine, dialogueBody)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	resolve := func(string) (*engine.Spec, error) { return spec, nil }
	replayed, err := eng.ReplaySession("s_dialogue", "tester", sink.events, resolve, nopSink{})
	if err != nil {
		t.Fatalf("ReplaySession: %v", err)
	}
	if replayed.DialogueMode != model.DialogueAutonomous || replayed.DialogueSource != "config.local.yaml" {
		t.Errorf("a resumed session should keep the mode it opened with, got %q from %q", replayed.DialogueMode, replayed.DialogueSource)
	}
}

func TestGuidedSessionHeaderIsUnchanged(t *testing.T) {
	sink := &recordingSink{}
	dialogueSession(t, sink, engine.WithDialogue(model.DialogueGuided, "config.local.yaml"))
	if _, ok := sink.events[0].Data["dialogue"]; ok {
		t.Errorf("a guided session's header should carry no mode, got %+v", sink.events[0].Data)
	}
}

func TestReasonAnswersOnlyAUserChooser(t *testing.T) {
	machine := `steps:
    - id: judge
      chooser: agent
      options:
          - {choice: go, to: end(completed)}`
	spec := mustParseLanes(t, machine, "## unit: judge\n\nJudge.\n")
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	session := eng.NewSession("s_dialogue", "tester", nopSink{}, autonomous())
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := session.AnswerWith(sv.Instance, "judge", "go", nil, engine.AnswerWords{Reason: "obvious"}); err == nil || !strings.Contains(err.Error(), "only a user chooser") {
		t.Errorf("a reason at an agent chooser should be refused, got %v", err)
	}
}

func TestDialogueModeNameStaysTheEngines(t *testing.T) {
	declarations := map[string]string{
		"state":  "state:\n    dialogueMode: {type: text, optional: true, desc: x}\n    note: {type: text, desc: x}\n",
		"params": "params:\n    dialogueMode: {type: text, optional: true, desc: x}\nstate:\n    note: {type: text, desc: x}\n",
	}
	for block, declared := range declarations {
		t.Run(block, func(t *testing.T) {
			machine := declared + `steps:
    - id: draft
      collect: [note]
      transitions:
          - when: hasBody
            to: end(completed)`
			spec := mustParseLanes(t, machine, "## unit: draft\n\nDraft.\n")
			eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
			if _, err := eng.NewSession("s_dialogue", "tester", nopSink{}).Start(spec, nil, ""); err == nil || !strings.Contains(err.Error(), "dialogueMode") {
				t.Errorf("a procedure field named dialogueMode should fail the render instead of shadowing the mode, got %v", err)
			}
		})
	}
}

func TestDelegableOnlyOnAUserChooser(t *testing.T) {
	machine := `steps:
    - id: pick
      chooser: agent
      delegable: true
      options:
          - {choice: go, to: end(completed)}`
	requireProblem(t, parseFails(t, machine, "## unit: pick\n\nPick.\n"), "delegable applies only to a user chooser")
}
