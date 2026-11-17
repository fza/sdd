package engine_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// assistedMachine holds a delegable choice, a check's unattended chooser and
// the approval gate: the one step confirming playback.
const assistedMachine = `state:
    body: {type: text, optional: true, desc: x}
steps:
    - id: pick
      chooser: user
      delegable: true
      options:
          - {choice: go, to: gate}
          - {choice: check, to: blocked}
    - id: blocked
      chooser: user
      unattended: {answer: revise, limit: 1}
      options:
          - {choice: revise, to: gate}
          - {choice: override, to: gate}
    - id: gate
      chooser: user
      delegable: true
      serveDelta: [body]
      options:
          - {choice: confirm, call: confirmPlayback, to: written}
          - {choice: adjust, collect: ["body?"], to: pick}
          - {choice: abort, to: end(abandoned)}
    - id: written
      guard: playbackConfirmed
      transitions:
          - otherwise: end(completed)`

const assistedBody = "## unit: pick\n\n{{if eq .dialogueMode \"auto\" \"assisted\"}}Decide yourself.{{else}}Ask the user.{{end}}\n\n" +
	"## unit: blocked\n\nRevise.\n\n" +
	"## unit: gate\n\n{{if eq .dialogueMode \"assisted\"}}Play it back to the person.{{else}}Confirm.{{end}}\n"

func assisted() engine.SessionOption {
	return engine.WithDialogue(model.DialogueAssisted, "config.local.yaml")
}

func assistedSession(t *testing.T, sink engine.EventSink, opts ...engine.SessionOption) (*engine.Session, *engine.Serve) {
	t.Helper()
	spec := mustParseLanes(t, assistedMachine, assistedBody)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	session := eng.NewSession("s_assisted", "tester", sink, opts...)
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return session, sv
}

// toGate lets the agent take the delegable choice in an assisted session.
func toGate(t *testing.T, session *engine.Session, instance string) *engine.Serve {
	t.Helper()
	sv, err := session.AnswerWith(instance, "pick", "go", nil, engine.AnswerWords{Reason: "the draft is ready"})
	if err != nil {
		t.Fatalf("the agent should take a delegable choice in an assisted session: %v", err)
	}
	if sv.Step != "gate" {
		t.Fatalf("the answer should reach the gate, got %s", sv.Step)
	}
	return sv
}

func TestAssistedAgentTakesTheChoicesBeforeTheGate(t *testing.T) {
	session, sv := assistedSession(t, nopSink{}, assisted())
	if sv.UnitText != "Decide yourself." || !strings.HasPrefix(sv.Goal, "decide yourself") || !schemaHas(sv, "reason") {
		t.Fatalf("a delegable choice should be the agent's in an assisted session, got text %q goal %q schema %v", sv.UnitText, sv.Goal, sv.ReportSchema)
	}
	sv, err := session.AnswerWith(sv.Instance, "pick", "check", nil, engine.AnswerWords{Reason: "run the check"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.AnswerWith(sv.Instance, "blocked", "override", nil, engine.AnswerWords{Reason: "it does not apply"}); err == nil || !strings.Contains(err.Error(), `blocked refuses "override"`) {
		t.Errorf("an unattended step should bind the agent in an assisted session as in auto, got %v", err)
	}
}

func TestAssistedGateIsThePersons(t *testing.T) {
	session, sv := assistedSession(t, nopSink{}, assisted())
	sv = toGate(t, session, sv.Instance)
	if !strings.HasPrefix(sv.UnitText, "Play it back to the person.") || !strings.HasPrefix(sv.Goal, "play the checked draft back to the person") {
		t.Errorf("the gate should address the person, got text %q goal %q", sv.UnitText, sv.Goal)
	}
	if schemaHas(sv, "reason") || !schemaRequires(sv, "userWords") || !schemaHas(sv, "batch") {
		t.Errorf("the gate should take the person's words, no reason, and name a batch, got %v", sv.ReportSchema)
	}
	if _, err := session.AnswerWith(sv.Instance, "gate", "confirm", nil, engine.AnswerWords{Reason: "the draft holds"}); err == nil || !strings.Contains(err.Error(), "gate is the person's approval in an assisted session") {
		t.Fatalf("the agent's reason should be refused at the gate, got %v", err)
	}
	if _, err := session.AnswerWith(sv.Instance, "gate", "confirm", nil, engine.AnswerWords{}); err == nil || !strings.Contains(err.Error(), "gate is the person's approval in an assisted session") {
		t.Fatalf("an answer without the person's words should be refused at the gate, got %v", err)
	}
	inst, _ := session.Instance(sv.Instance)
	if engine.Approved(inst.Store) {
		t.Fatal("nothing should count as approved before the person answers")
	}
	sv, err := session.AnswerWith(sv.Instance, "gate", "confirm", nil, engine.AnswerWords{User: "ok, write it"})
	if err != nil {
		t.Fatal(err)
	}
	if sv.Status != engine.StatusCompleted || !engine.Approved(inst.Store) {
		t.Errorf("the person's confirm should approve the draft and pass the gate, got status %s approved %v", sv.Status, engine.Approved(inst.Store))
	}
}

// An approval binds to the state the person saw: a change after it voids it.
func TestAssistedApprovalBindsToTheState(t *testing.T) {
	spec := mustParseLanes(t, strings.Replace(assistedMachine, "to: written}", "to: hold}", 1)+`
    - id: hold
      collect: ["body?"]
      transitions:
          - when: ready
            to: written`, assistedBody+"\n## unit: hold\n\nHold.\n")
	w := newGateWorld(t)
	session := w.engine.NewSession("s_assisted", "tester", &memorySink{}, assisted())
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	sv = toGate(t, session, sv.Instance)
	if _, err := session.AnswerWith(sv.Instance, "gate", "confirm", nil, engine.AnswerWords{User: "approved"}); err != nil {
		t.Fatal(err)
	}
	inst, _ := session.Instance(sv.Instance)
	if !engine.Approved(inst.Store) {
		t.Fatal("the person's confirm should approve the state")
	}
	if _, err := session.Report(sv.Instance, map[string]any{"body": "changed after the approval"}); err != nil {
		t.Fatal(err)
	}
	if engine.Approved(inst.Store) {
		t.Error("a change after the approval should void it")
	}
}

func TestOnlyAnAssistedSessionHasTheGate(t *testing.T) {
	cases := map[string][]engine.SessionOption{"guided": nil, "auto": {autonomous()}}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			session, sv := assistedSession(t, nopSink{}, opts...)
			words := engine.AnswerWords{User: "go on"}
			if name == "auto" {
				words = engine.AnswerWords{Reason: "ready"}
			}
			sv, err := session.AnswerWith(sv.Instance, "pick", "go", nil, words)
			if err != nil {
				t.Fatal(err)
			}
			if schemaHas(sv, "batch") {
				t.Errorf("only an assisted session should offer a batch, got %v", sv.ReportSchema)
			}
			inst, _ := session.Instance(sv.Instance)
			if _, err := session.AnswerWith(sv.Instance, "gate", "confirm", nil, words); err != nil {
				t.Fatal(err)
			}
			if engine.Approved(inst.Store) {
				t.Errorf("a %s confirm should not count as a person's approval", name)
			}
		})
	}
}

// The gate holds the draft for the person on arrival: a session that ends
// unanswered leaves it listed as parked, and the person's answer ends that.
func TestAssistedGateParksTheDraftOnArrival(t *testing.T) {
	sink := &recordingSink{}
	session, sv := assistedSession(t, sink, assisted())
	sv = toGate(t, session, sv.Instance)
	if sv.Parked != nil || sv.Chooser == nil {
		t.Fatalf("the gate should still serve its chooser, got parked %+v chooser %+v", sv.Parked, sv.Chooser)
	}
	if _, err := session.Serve(sv.Instance); err != nil {
		t.Fatal(err)
	}
	var parked []engine.Event
	for _, ev := range sink.events {
		if ev.Event == engine.EventParked {
			parked = append(parked, ev)
		}
	}
	if len(parked) != 1 || parked[0].Data["step"] != "gate" || parked[0].Data["held"] != true || parked[0].Data["reason"] == "" {
		t.Fatalf("arriving at the gate should log one held park at it with a reason, got %+v", parked)
	}
}

func TestAssistedAgentCannotAbandonASealedDraft(t *testing.T) {
	session, sv := assistedSession(t, nopSink{}, assisted())
	sv = toGate(t, session, sv.Instance)
	if err := session.RefuseUnattendedAbandon(sv.Instance); err == nil || !strings.Contains(err.Error(), "waits for the person's approval") {
		t.Errorf("a draft at the gate should not be dropped by the agent, got %v", err)
	}
}

func TestAssistedSessionHeaderSurvivesReplay(t *testing.T) {
	sink := &recordingSink{}
	assistedSession(t, sink, assisted())
	if meta := sink.events[0]; meta.Data["dialogue"] != "assisted" {
		t.Fatalf("the session header should record the assisted mode, got %+v", meta)
	}
	spec := mustParseLanes(t, assistedMachine, assistedBody)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	replayed, err := eng.ReplaySession("s_assisted", "tester", sink.events, func(string) (*engine.Spec, error) { return spec, nil }, nopSink{})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.DialogueMode != model.DialogueAssisted {
		t.Errorf("a resumed session should keep the assisted mode, got %q", replayed.DialogueMode)
	}
}

// Sealed lists the drafts waiting at their gate; CheckSealed admits only
// those into one person's answer.
func TestAssistedSealedDrafts(t *testing.T) {
	spec := mustParseLanes(t, assistedMachine, assistedBody)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	session := eng.NewSession("s_assisted", "tester", nopSink{}, assisted())
	var instances []string
	for range 3 {
		sv, err := session.Start(spec, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, sv.Instance)
	}
	toGate(t, session, instances[0])
	toGate(t, session, instances[1])
	if got := session.Sealed(); !slices.Equal(got, instances[:2]) {
		t.Errorf("the drafts at their gate should be sealed, got %v want %v", got, instances[:2])
	}
	if err := session.CheckSealed(instances[1], "gate"); err != nil {
		t.Errorf("a draft at its gate should join an answer: %v", err)
	}
	if err := session.CheckSealed(instances[2], "gate"); err == nil || !strings.Contains(err.Error(), "not sealed") {
		t.Errorf("a draft before its gate should not join an answer, got %v", err)
	}
	if err := session.CheckSealed("nope", "gate"); err == nil {
		t.Error("an unknown instance should not join an answer")
	}
	if err := session.Update(instances[0], map[string]any{"body": "changed after the playback"}); err != nil {
		t.Fatal(err)
	}
	if err := session.CheckSealed(instances[0], "gate"); err == nil || !strings.Contains(err.Error(), "changed since it was played back") {
		t.Errorf("a draft changed since its playback should not join an answer, got %v", err)
	}
	session.SetDialogue(model.DialogueAuto, "")
	if got := session.Sealed(); len(got) != 0 {
		t.Errorf("outside an assisted session nothing is sealed, got %v", got)
	}
	if err := session.CheckSealed(instances[1], "gate"); err == nil {
		t.Error("outside an assisted session no draft should join an answer")
	}
}

// A draft the person sends back unrevised holds where it returns until it
// changes: their adjust asks for a revision.
func TestAssistedSendBackHoldsUntilRevised(t *testing.T) {
	machine := `state:
    body: {type: text, optional: true, desc: x}
steps:
    - id: draft
      guard: revisedSinceGate
      collect: ["body?"]
      transitions:
          - otherwise: gate
    - id: gate
      chooser: user
      options:
          - {choice: confirm, call: confirmPlayback, to: end(completed)}
          - {choice: adjust, collect: ["body?"], to: draft}`
	spec := mustParseLanes(t, machine, "## unit: draft\n\nDraft.\n\n## unit: gate\n\nPlay back.\n")
	for name, opts := range map[string][]engine.SessionOption{"assisted": {assisted()}, "guided": nil} {
		t.Run(name, func(t *testing.T) {
			sink := &recordingSink{}
			eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
			session := eng.NewSession("s_assisted", "tester", sink, opts...)
			sv, err := session.Start(spec, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if sv.Step != "gate" {
				t.Fatalf("a draft never played back should pass, got %s", sv.Step)
			}
			sv, err = session.Answer(sv.Instance, "gate", "adjust", nil, "not this one")
			if err != nil {
				t.Fatal(err)
			}
			if name == "guided" {
				if sv.Step != "gate" {
					t.Errorf("a guided session holds no draft for its revision, got %s", sv.Step)
				}
				return
			}
			if sv.Step != "draft" || !slices.ContainsFunc(sv.Diagnostics, func(d string) bool { return strings.Contains(d, "sent this draft back") }) {
				t.Fatalf("the unrevised draft should hold, got step %s diagnostics %v", sv.Step, sv.Diagnostics)
			}
			replayed, err := eng.ReplaySession("s_assisted", "tester", sink.events, func(string) (*engine.Spec, error) { return spec, nil }, nopSink{})
			if err != nil {
				t.Fatal(err)
			}
			if sv, err = replayed.Serve(sv.Instance); err != nil || sv.Step != "draft" || len(sv.Diagnostics) == 0 {
				t.Fatalf("a resumed session should keep holding the draft, got step %s diagnostics %v err %v", sv.Step, sv.Diagnostics, err)
			}
			session.SetDialogue(model.DialogueGuided, "")
			if released, err := session.Poll(sv.Instance); err != nil || released.Step != "gate" {
				t.Fatalf("outside an assisted session no gate holds the draft, got %+v err %v", released, err)
			}
			sv, err = replayed.Report(sv.Instance, map[string]any{"body": "revised"})
			if err != nil {
				t.Fatal(err)
			}
			if sv.Step != "gate" {
				t.Errorf("the revised draft should go on to the gate, got %s", sv.Step)
			}
		})
	}
}

// Approving tells a person's approval at the gate from every other answer.
func TestAssistedApprovingNamesThePersonsApproval(t *testing.T) {
	session, sv := assistedSession(t, nopSink{}, assisted())
	if session.Approving(sv.Instance, "pick", "go") {
		t.Error("a choice before the gate should not approve")
	}
	sv = toGate(t, session, sv.Instance)
	if !session.Approving(sv.Instance, "gate", "confirm") {
		t.Error("the confirm at the gate should be the person's approval")
	}
	if session.Approving(sv.Instance, "gate", "adjust") || session.Approving("nope", "gate", "confirm") {
		t.Error("only the confirming option of a known draft should approve")
	}
	session.SetDialogue(model.DialogueGuided, "")
	if session.Approving(sv.Instance, "gate", "confirm") {
		t.Error("outside an assisted session no confirm is an approval")
	}
}

// A park at the gate is told apart from every other park.
func TestAssistedGateParkIsMarked(t *testing.T) {
	sink := &recordingSink{}
	session, sv := assistedSession(t, sink, assisted())
	toGate(t, session, sv.Instance)
	marked := 0
	for _, ev := range sink.events {
		if engine.ParkedForApproval(ev) {
			marked++
		}
	}
	if marked != 1 {
		t.Errorf("the gate's park should be marked as waiting for an approval once, got %d of %+v", marked, sink.events)
	}
}

// heldForApproval tells a step after the gate that the gate held the draft
// at its current state, when the session left assisted before the person
// answered.
func TestAssistedHeldForApproval(t *testing.T) {
	machine := `state:
    body: {type: text, optional: true, desc: x}
steps:
    - id: gate
      chooser: user
      delegable: true
      options:
          - {choice: confirm, call: confirmPlayback, to: limit}
    - id: limit
      chooser: user
      options:
          - {choice: done, to: end(completed)}`
	spec := mustParseLanes(t, machine, "## unit: gate\n\nPlay back.\n\n## unit: limit\n\n{{if .heldForApproval}}held{{else}}fresh{{end}}\n")
	for _, tc := range []struct {
		name    string
		opts    []engine.SessionOption
		changed bool
		want    string
	}{
		{name: "held at the gate", opts: []engine.SessionOption{assisted()}, want: "held"},
		{name: "changed since", opts: []engine.SessionOption{assisted()}, changed: true, want: "fresh"},
		{name: "never held", opts: []engine.SessionOption{autonomous()}, want: "fresh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
			session := eng.NewSession("s_held", "tester", nopSink{}, tc.opts...)
			sv, err := session.Start(spec, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			session.SetDialogue(model.DialogueAuto, "")
			if tc.changed {
				if err := session.Update(sv.Instance, map[string]any{"body": "changed"}); err != nil {
					t.Fatal(err)
				}
			}
			sv, err = session.AnswerWith(sv.Instance, "gate", "confirm", nil, engine.AnswerWords{Reason: "holds"})
			if err != nil {
				t.Fatal(err)
			}
			if sv.UnitText != tc.want {
				t.Errorf("the step after the gate should read %q, got %q", tc.want, sv.UnitText)
			}
		})
	}
}

// A batch lists a draft by its id, or with the confirming option's fields
// for that draft alone.
func TestAssistedBatchItemCarriesTheConfirmingFields(t *testing.T) {
	spec := mustParseLanes(t, strings.Replace(assistedMachine, "{choice: confirm, call: confirmPlayback", `{choice: confirm, collect: ["body?"], call: confirmPlayback`, 1), assistedBody)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	session := eng.NewSession("s_assisted", "tester", nopSink{}, assisted())
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	sv = toGate(t, session, sv.Instance)
	props, _ := sv.ReportSchema["properties"].(map[string]any)
	batch, _ := props["batch"].(map[string]any)
	lists, _ := batch["additionalProperties"].(map[string]any)
	items, _ := lists["items"].(map[string]any)
	forms, _ := items["anyOf"].([]any)
	if len(forms) != 2 {
		t.Fatalf("a batch item should be an id or an object, got %v", items)
	}
	object, _ := forms[1].(map[string]any)
	fields, _ := object["properties"].(map[string]any)
	if _, ok := fields["instance"]; !ok {
		t.Errorf("an item object should name its instance, got %v", fields)
	}
	if _, ok := fields["body"]; !ok {
		t.Errorf("an item object should carry the confirming option's fields, got %v", fields)
	}
}
