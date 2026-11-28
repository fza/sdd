package engine_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const draftServeMachine = `state:
    body: {type: prose, desc: the draft}
    topics: {type: list<label>, optional: true, desc: labels}
    layer: {type: layer, optional: true, desc: layer}
    goal: {type: text, optional: true, desc: closer}
steps:
    - id: draft
      collect: [body, "topics?", "layer?", "goal?"]
      serveDelta: [body, topics, layer]
      transitions:
          - when: hasGoal
            to: end(completed)`

func draftServeEntry(t *testing.T) *model.Entry {
	t.Helper()
	content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: draftproc\n" +
		draftServeMachine + "\n---\n\ndraft procedure\n\n## unit: draft\n\nDraft guidance.\n"
	entry, err := model.ParseEntry("20260826-140000-d-prc-drf.md", content)
	if err != nil {
		t.Fatalf("fixture entry: %v", err)
	}
	return entry
}

func longBody() string {
	return strings.Join([]string{
		"line one", "line two", "line three", "line four", "line five",
		"line six", "line seven", "line eight", "line nine", "line ten",
		"line eleven", "line twelve", "line thirteen", "line fourteen",
	}, "\n")
}

// The draft lane is what the agent wrote, so no text question asks about it
// (20261121-230157-d-tac-ym8); the step's own lanes stay askable.
func TestDraftServe_TheDraftLaneIsAuthored(t *testing.T) {
	spec, err := engine.ParseSpec(draftServeEntry(t))
	if err != nil {
		t.Fatal(err)
	}
	sess := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_authored", "tester", &recordingSink{})
	sv, err := sess.Start(spec, map[string]any{"body": longBody()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sv.Lanes) < 2 || sv.Lanes[0].Name != engine.DraftLane {
		t.Fatalf("the serve should lead with the draft lane, got %+v", sv.Lanes)
	}
	if !sv.Lanes[0].Authored {
		t.Error("the draft lane should be marked authored")
	}
	for _, lane := range sv.Lanes[1:] {
		if lane.Authored {
			t.Errorf("the step's own lanes should stay askable, got %s marked authored", lane.Name)
		}
	}
}

// TestDraftServe_Rounds drives the full arc: bounded first round, item-level
// list delta, scalar whole-when-changed, prose content diff, an explicit
// nothing-changed round, and the rehydrate serve whole.
func TestDraftServe_Rounds(t *testing.T) {
	spec, err := engine.ParseSpec(draftServeEntry(t))
	if err != nil {
		t.Fatal(err)
	}
	sess := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_rounds", "tester", &recordingSink{})
	sv, err := sess.Start(spec, map[string]any{"body": longBody(), "topics": []any{"cli/ux"}, "layer": "tactical"}, "")
	if err != nil {
		t.Fatal(err)
	}
	instance := sv.Instance

	// Round one: whole block, prose bounded with the middle marked elided.
	if !strings.Contains(sv.UnitText, "lines elided") {
		t.Fatalf("first round should bound the prose body, got %q", sv.UnitText)
	}
	if !strings.Contains(sv.UnitText, "- layer: tactical") || !strings.Contains(sv.UnitText, `- topics: "cli/ux"`) {
		t.Fatalf("first round should serve small fields whole, got %q", sv.UnitText)
	}

	// A list edit serves item-level: one added, one removed.
	sv, err = sess.Report(instance, map[string]any{"topics": []any{"engine/serve"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sv.UnitText, "Draft delta since last served") ||
		!strings.Contains(sv.UnitText, `+ "engine/serve"`) || !strings.Contains(sv.UnitText, `- "cli/ux"`) {
		t.Fatalf("list delta should be item-level, got %q", sv.UnitText)
	}
	if strings.Contains(sv.UnitText, "line one") {
		t.Fatalf("unchanged prose must not re-serve, got %q", sv.UnitText)
	}
	if !strings.Contains(sv.UnitText, "unchanged: body, layer") {
		t.Fatalf("delta should name unchanged fields, got %q", sv.UnitText)
	}

	// A prose edit serves as a content diff, not the whole text.
	edited := strings.Replace(longBody(), "line seven", "line SEVEN edited", 1)
	sv, err = sess.Report(instance, map[string]any{"body": edited})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sv.UnitText, "-line seven") || !strings.Contains(sv.UnitText, "+line SEVEN edited") {
		t.Fatalf("prose delta should be a content diff, got %q", sv.UnitText)
	}
	if strings.Contains(sv.UnitText, "line twelve") {
		t.Fatalf("a content diff must not carry distant unchanged lines, got %q", sv.UnitText)
	}

	// A round in which nothing changed says so explicitly.
	sv, err = sess.Report(instance, map[string]any{"body": edited})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sv.UnitText, "Draft unchanged since last served") {
		t.Fatalf("a no-change round must say so, got %q", sv.UnitText)
	}

	// The rehydrate path serves the draft whole — prose included, unbounded.
	sv, err = sess.Serve(instance)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sv.UnitText, "Draft as it stands") || !strings.Contains(sv.UnitText, "line twelve") ||
		strings.Contains(sv.UnitText, "lines elided") {
		t.Fatalf("rehydrate must serve the draft whole, got %q", sv.UnitText)
	}

	// The full serve reset the base: an unchanged round after it still deltas.
	sv, err = sess.Report(instance, map[string]any{"layer": "process"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sv.UnitText, "- layer: process") || strings.Contains(sv.UnitText, "line one") {
		t.Fatalf("post-rehydrate delta should carry only the change, got %q", sv.UnitText)
	}
}

// The draft lane leads the serve, ahead of the step's own guidance.
func TestDraftServe_TheDraftLeads(t *testing.T) {
	spec, err := engine.ParseSpec(draftServeEntry(t))
	if err != nil {
		t.Fatal(err)
	}
	sess := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_leads", "tester", &recordingSink{})
	sv, err := sess.Start(spec, map[string]any{"body": longBody()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sv.Lanes) < 2 || sv.Lanes[0].Name != engine.DraftLane {
		t.Fatalf("the draft lane should lead, got %+v", sv.Lanes)
	}
	if !strings.HasPrefix(sv.UnitText, "Draft as it stands") || !strings.HasSuffix(sv.UnitText, "Draft guidance.") {
		t.Errorf("the unit text should open with the draft, then the guidance, got %q", sv.UnitText)
	}
}

func TestDraftServe_UpdateKeepsTheServedBase(t *testing.T) {
	spec, err := engine.ParseSpec(draftServeEntry(t))
	if err != nil {
		t.Fatal(err)
	}
	sess := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_update", "tester", &recordingSink{})
	sv, err := sess.Start(spec, map[string]any{"body": longBody(), "topics": []any{"cli/ux"}, "layer": "tactical"}, "")
	if err != nil {
		t.Fatal(err)
	}

	edited := strings.Replace(longBody(), "line seven", "line SEVEN updated", 1)
	if err := sess.Update(sv.Instance, map[string]any{"body": edited}); err != nil {
		t.Fatal(err)
	}
	sv, err = sess.Report(sv.Instance, map[string]any{"layer": "process"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sv.UnitText, "+line SEVEN updated") {
		t.Errorf("an update should stay in the next delta, as nothing served it, got %q", sv.UnitText)
	}
}

const unservedDraftMachine = `state:
    body: {type: prose, desc: the draft}
steps:
    - id: playback
      chooser: user
      serveDelta: [body]
      options:
          - {choice: confirm, call: confirmPlayback, to: end(completed)}`

// A host replays the session before each call; the unserved mark and its
// playback both ride the log.
func TestDraftServe_UnservedChangeSurvivesAReplay(t *testing.T) {
	content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: unservedproc\n" +
		unservedDraftMachine + "\n---\n\nunserved draft procedure\n\n## unit: playback\n\nPlay the draft back.\n"
	entry, err := model.ParseEntry("20260826-140000-d-prc-uns.md", content)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := engine.ParseSpec(entry)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	log := &recordingSink{}
	replay := func() *engine.Session {
		t.Helper()
		sess, err := eng.ReplaySession("s_unserved", "tester", log.events, func(string) (*engine.Spec, error) { return spec, nil }, log)
		if err != nil {
			t.Fatal(err)
		}
		return sess
	}
	sv, err := eng.NewSession("s_unserved", "tester", log).Start(spec, map[string]any{"body": "first draft"}, "")
	if err != nil {
		t.Fatal(err)
	}
	instance := sv.Instance
	if err := replay().Update(instance, map[string]any{"body": "second draft"}); err != nil {
		t.Fatal(err)
	}

	sess := replay()
	sv, err = sess.Answer(instance, "playback", "confirm", nil, "yes")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Status != engine.StatusRunning || !strings.Contains(sv.Instructions, "not answered") || !sv.PlaysBack {
		t.Fatalf("an answer over an unserved change should be refused after a replay too, got %s %q", sv.Status, sv.Instructions)
	}
	sess.RecordPlayback(instance)
	sv, err = replay().Answer(instance, "playback", "confirm", nil, "yes")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Status != engine.StatusCompleted {
		t.Errorf("an answer after the change was played back should apply after a replay, got %s", sv.Status)
	}
}

func TestDraftServe_AnswerOverAnUnservedChangeServesItInstead(t *testing.T) {
	content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: unservedproc\n" +
		unservedDraftMachine + "\n---\n\nunserved draft procedure\n\n## unit: playback\n\nPlay the draft back.\n"
	entry, err := model.ParseEntry("20260826-140000-d-prc-uns.md", content)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := engine.ParseSpec(entry)
	if err != nil {
		t.Fatal(err)
	}
	sess := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_unserved", "tester", &recordingSink{})
	sv, err := sess.Start(spec, map[string]any{"body": "first draft"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Update(sv.Instance, map[string]any{"body": "second draft"}); err != nil {
		t.Fatal(err)
	}

	sv, err = sess.Answer(sv.Instance, "playback", "confirm", nil, "yes")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Status != engine.StatusRunning || !strings.Contains(sv.UnitText, "second draft") || !strings.Contains(sv.Instructions, "not answered") {
		t.Fatalf("an answer over an unserved change should serve the change and not apply, got %s %q", sv.Status, sv.Instructions)
	}
	sv, err = sess.Answer(sv.Instance, "playback", "confirm", nil, "yes")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Status != engine.StatusRunning || !sv.PlaysBack {
		t.Fatalf("an answer over a change served but not delivered should be refused again, got %s", sv.Status)
	}

	sess.RecordPlayback(sv.Instance)
	sv, err = sess.Answer(sv.Instance, "playback", "confirm", nil, "yes")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Status != engine.StatusCompleted {
		t.Errorf("an answer once the change was served should apply, got %s", sv.Status)
	}
}

// A serve that fails after rendering the draft delivered nothing, so it must
// leave the change unserved.
func TestDraftServe_FailedServeLeavesTheChangeUnserved(t *testing.T) {
	const machine = `state:
    body: {type: prose, desc: the draft}
steps:
    - id: review
      serveDelta: [body]
      guard: reviewHolds
      transitions:
          - otherwise: end(completed)`
	content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: failedserveproc\n" +
		machine + "\n---\n\nfailed serve procedure\n\n## unit: review\n\nReview the draft.\n"
	entry, err := model.ParseEntry("20260826-140000-d-prc-fsv.md", content)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := engine.ParseSpec(entry)
	if err != nil {
		t.Fatal(err)
	}
	broken := false
	registry := engine.NewRegistry()
	err = registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{Name: "reviewHolds"},
		Fn: func(*engine.Context) (bool, error) {
			if broken {
				return false, errors.New("the check is unavailable")
			}
			return false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	sess := engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_failed_serve", "tester", sink)
	sv, err := sess.Start(spec, map[string]any{"body": "first draft"}, "")
	if err != nil {
		t.Fatal(err)
	}
	instance := sv.Instance
	if err := sess.Update(instance, map[string]any{"body": "second draft"}); err != nil {
		t.Fatal(err)
	}

	broken = true
	if _, err := sess.Serve(instance); err == nil {
		t.Fatal("a serve whose guard fails to evaluate should fail")
	}
	for _, ev := range sink.events {
		if ev.Event == engine.EventServed && ev.Data["playedBack"] == true {
			t.Fatalf("a failed serve should log no playback, got %+v", ev)
		}
	}
	broken = false
	sv, err = sess.Serve(instance)
	if err != nil {
		t.Fatal(err)
	}
	if !sv.PlaysBack {
		t.Error("the serve after a failed one should still play the change back")
	}
}

const redraftMachine = `state:
    body: {type: prose, desc: the draft}
steps:
    - id: draft
      collect: [body]
      transitions:
          - when: hasBody
            to: playback
    - id: playback
      chooser: user
      serveDelta: [body]
      redraft: draft
      options:
          - {choice: confirm, call: confirmPlayback, to: end(completed)}
          - {choice: adjust, collect: ["body?"], to: draft}
          - {choice: abort, to: end(abandoned)}`

func redraftSession(t *testing.T, opts ...engine.SessionOption) (*engine.Session, *recordingSink, string) {
	t.Helper()
	content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: redraftproc\n" +
		redraftMachine + "\n---\n\nredraft procedure\n\n## unit: draft\n\nDraft it.\n\n## unit: playback\n\nPlay the draft back.\n"
	entry, err := model.ParseEntry("20260826-140000-d-prc-rdr.md", content)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := engine.ParseSpec(entry)
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	sess := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_redraft", "tester", sink, opts...)
	sv, err := sess.Start(spec, map[string]any{"body": "first draft"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Step != "playback" {
		t.Fatalf("the draft should reach its playback, got %s", sv.Step)
	}
	if err := sess.Update(sv.Instance, map[string]any{"body": "second draft"}); err != nil {
		t.Fatal(err)
	}
	return sess, sink, sv.Instance
}

// A confirm over an unserved change goes to the redraft step, so the change is
// drafted and checked again before any confirm binds to it.
func TestDraftServe_AConfirmOverAnUnservedChangeRedrafts(t *testing.T) {
	sess, sink, instance := redraftSession(t)
	sv, err := sess.Answer(instance, "playback", "confirm", nil, "yes")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Status != engine.StatusRunning || !strings.Contains(sv.Instructions, "went back to draft") || !sv.PlaysBack {
		t.Fatalf("a confirm over an unserved change should not apply, got %s %q", sv.Status, sv.Instructions)
	}
	redrafted := false
	for _, ev := range sink.events {
		if ev.Event == engine.EventTransition && ev.Data["from"] == "playback" && ev.Data["to"] == "draft" {
			redrafted = true
		}
	}
	if !redrafted {
		t.Error("the change should pass through the redraft step")
	}
	sess.RecordPlayback(instance)
	sv, err = sess.Answer(instance, "playback", "confirm", nil, "yes")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Status != engine.StatusCompleted {
		t.Errorf("a confirm once the change was played back should apply, got %s", sv.Status)
	}
}

// Only a confirm binds to the draft: an answer revising or dropping it stands
// over an unserved change.
func TestDraftServe_AnAnswerOtherThanAConfirmStandsOverAnUnservedChange(t *testing.T) {
	tests := []struct {
		choice     string
		fields     map[string]any
		wantStep   string
		wantStatus engine.InstanceStatus
	}{
		{choice: "adjust", fields: map[string]any{"body": "second draft"}, wantStep: "playback", wantStatus: engine.StatusRunning},
		{choice: "abort", wantStatus: engine.StatusAbandoned},
	}
	for _, tt := range tests {
		t.Run(tt.choice, func(t *testing.T) {
			sess, _, instance := redraftSession(t)
			sv, err := sess.Answer(instance, "playback", tt.choice, tt.fields, "this one")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(sv.Instructions, "not answered") || sv.Status != tt.wantStatus || (tt.wantStep != "" && sv.Step != tt.wantStep) {
				t.Fatalf("the answer should apply, got %s at %s: %q", sv.Status, sv.Step, sv.Instructions)
			}
		})
	}
}

func TestDraftServe_ARedraftNeedsADraftStepAndATarget(t *testing.T) {
	tests := []struct {
		name, from, to, want string
	}{
		{name: "a step without a serveDelta", from: "      serveDelta: [body]\n      redraft: draft\n", to: "      redraft: draft\n", want: "redraft applies only to a step with a serveDelta"},
		{name: "an unknown target", from: "redraft: draft", to: "redraft: nowhere", want: `redraft target "nowhere" is not a step`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			machine := strings.Replace(redraftMachine, tt.from, tt.to, 1)
			content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: redraftproc\n" +
				machine + "\n---\n\nredraft procedure\n\n## unit: draft\n\nDraft it.\n\n## unit: playback\n\nPlay the draft back.\n"
			entry, err := model.ParseEntry("20260826-140000-d-prc-rdr.md", content)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.ParseSpec(entry); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("the spec should be refused naming the redraft, got %v", err)
			}
		})
	}
}

// A redraft returning to an assisted session's approval gate holds the draft
// there afresh, so the gate binds to the redrafted draft.
func TestDraftServe_ARedraftReturnsToTheApprovalGateHeldAfresh(t *testing.T) {
	sess, sink, instance := redraftSession(t, assisted())
	if _, err := sess.Answer(instance, "playback", "confirm", nil, "yes"); err != nil {
		t.Fatal(err)
	}
	holds := 0
	for _, ev := range sink.events {
		if engine.ParkedForApproval(ev) {
			holds++
		}
	}
	if holds != 2 {
		t.Errorf("the draft should be held at the gate on its arrival and again on its return, got %d holds", holds)
	}
}

// A session replayed while the redrafted draft is still being checked holds
// it at the approval gate afresh on its return, as the live session does.
func TestDraftServe_AReplayedRedraftReturnsToTheApprovalGateHeldAfresh(t *testing.T) {
	const machine = `state:
    body: {type: prose, desc: the draft}
    note: {type: text, optional: true, desc: a note}
steps:
    - id: draft
      collect: [body, "note?"]
      guard: checksPass
      transitions:
          - otherwise: playback
    - id: playback
      chooser: user
      serveDelta: [body]
      redraft: draft
      options:
          - {choice: confirm, call: confirmPlayback, to: end(completed)}`
	content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: redraftproc\n" +
		machine + "\n---\n\nredraft procedure\n\n## unit: draft\n\nDraft it.\n\n## unit: playback\n\nPlay the draft back.\n"
	entry, err := model.ParseEntry("20260826-140000-d-prc-rdr.md", content)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := engine.ParseSpec(entry)
	if err != nil {
		t.Fatal(err)
	}
	pass := true
	registry := engine.NewRegistry()
	if err := registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{Name: "checksPass"},
		Fn:  func(*engine.Context) (bool, error) { return pass, nil },
	}); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)})
	sink := &recordingSink{}
	sv, err := eng.NewSession("s_redraft_replay", "tester", sink, assisted()).Start(spec, map[string]any{"body": "first draft"}, "")
	if err != nil {
		t.Fatal(err)
	}
	instance := sv.Instance
	replay := func() *engine.Session {
		t.Helper()
		sess, err := eng.ReplaySession("s_redraft_replay", "tester", sink.events, func(string) (*engine.Spec, error) { return spec, nil }, sink, assisted())
		if err != nil {
			t.Fatal(err)
		}
		return sess
	}
	if err := replay().Update(instance, map[string]any{"body": "second draft"}); err != nil {
		t.Fatal(err)
	}
	pass = false
	if sv, err = replay().Answer(instance, "playback", "confirm", nil, "yes"); err != nil || sv.Step != "draft" {
		t.Fatalf("the redraft should wait at its checks, got %v at %v", err, sv)
	}
	pass = true
	if sv, err = replay().Report(instance, map[string]any{"note": "checked"}); err != nil || sv.Step != "playback" {
		t.Fatalf("the checked draft should return to the gate, got %v at %v", err, sv)
	}
	holds := 0
	for _, ev := range sink.events {
		if engine.ParkedForApproval(ev) {
			holds++
		}
	}
	if holds != 2 {
		t.Errorf("the replayed session should hold the returning draft at the gate afresh, got %d holds", holds)
	}
}
