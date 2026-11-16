package engine_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// A draft whose verifier delivers into received, a closing verdict one option
// collects, and a workflow only a procedure draft carries.
const schemaScopeMachine = `state:
    entryKind: {type: entry-kind, desc: the drafted kind}
    goal: {type: text, desc: what the review is for}
    workflow: {type: procedure-spec, optional: true, desc: the drafted workflow}
    received: {type: text, optional: true, desc: the delegate's findings}
    verdict: {type: text, optional: true, desc: the closing verdict}
steps:
    - id: draft
      collect: [entryKind, goal, "workflow?"]
      transitions:
          - when: hasGoal
            to: ask
    - id: ask
      chooser: agent
      options:
          - {choice: delegate, dispatch: {procedure: revisetask, seed: {topic: goal}, receive: {received: plan}}, to: wait}
    - id: wait
      await: [received]
      transitions:
          - otherwise: close
    - id: close
      chooser: agent
      options:
          - {choice: done, collect: [verdict], to: end(completed)}
          - {choice: rework, collect: ["workflow?"], to: draft}`

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the schema should carry properties, got %v", schema)
	}
	return props
}

func TestReportSchemaLeavesOutFieldsNoReportFills(t *testing.T) {
	spec := delegateSpec(t, "scoped", "move", schemaScopeMachine)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	courier := &sessionCourier{sessions: map[string]*engine.Session{}}
	session := eng.NewSession("s_scope", "tester", &recordingSink{}, engine.WithCourier(courier))
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	props := schemaProperties(t, sv.ReportSchema)
	for _, absent := range []string{"received", "verdict", "workflow"} {
		if _, ok := props[absent]; ok {
			t.Errorf("the draft schema should leave out %s, which no report fills here", absent)
		}
	}
	if _, ok := props["entryKind"]; !ok {
		t.Error("the draft schema should carry the step's own fields")
	}

	sv, err = session.Report(sv.Instance, map[string]any{"entryKind": "procedure"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := schemaProperties(t, sv.ReportSchema)["workflow"]; !ok {
		t.Error("a procedure draft's schema should carry the workflow")
	}
	inst, _ := session.Instance(sv.Instance)
	fields, _ := schemaProperties(t, spec.AnswerSchemaForStep(spec.StepByID["close"], model.DialogueGuided, inst.Store))["fields"].(map[string]any)
	if _, ok := schemaProperties(t, fields)["workflow"]; !ok {
		t.Error("a procedure draft's answer schema should carry the workflow")
	}

	sv, err = session.Report(sv.Instance, map[string]any{"entryKind": "gap", "goal": "check it"})
	if err != nil {
		t.Fatal(err)
	}
	sv, err = session.Answer(sv.Instance, "ask", "delegate", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Step != "wait" || sv.ReportSchema != nil {
		t.Fatalf("a step that only awaits a delivery should serve no report schema, at %s with %v", sv.Step, sv.ReportSchema)
	}
	inst, _ = session.Instance(sv.Instance)
	fields, _ = schemaProperties(t, spec.AnswerSchemaForStep(spec.StepByID["close"], model.DialogueGuided, inst.Store))["fields"].(map[string]any)
	if _, ok := schemaProperties(t, fields)["workflow"]; ok {
		t.Error("an answer schema should leave out the workflow while the draft is no procedure")
	}
}
