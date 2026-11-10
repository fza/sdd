package engine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const hostSetMachine = `state:
    body: {type: text, desc: the draft}
    stamp: {type: text, optional: true, hostSet: true, desc: written by the host}
    note: {type: text, optional: true, desc: a closing note}
steps:
    - id: draft
      collect: [body]
      transitions:
          - when: hasBody
            to: review
    - id: review
      chooser: user
      serveDelta: [body]
      options:
          - {choice: adjust, collect: ["body?", "note?"], to: review}
          - {choice: finish, to: end(completed)}`

func TestHostSet_StaysOutOfEverySchema(t *testing.T) {
	spec := historySpec(t, hostSetMachine)
	for _, step := range spec.Steps {
		schemas := []map[string]any{spec.ReportSchemaForStep(step)}
		if len(step.Options) > 0 {
			schemas = append(schemas, spec.AnswerSchemaForStep(step, model.DialogueGuided))
		}
		for _, schema := range schemas {
			encoded, err := json.Marshal(schema)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), `"stamp"`) {
				t.Errorf("a schema served at %s should not name the host-set field:\n%s", step.ID, encoded)
			}
		}
	}
	report, _ := spec.ReportSchemaForStep(spec.StepByID["draft"])["properties"].(map[string]any)
	if _, ok := report["note"]; !ok {
		t.Error("an ordinary state field should stay an accepted report property")
	}
}

func TestHostSet_AnswerCarriesItBeyondTheCollectList(t *testing.T) {
	spec := historySpec(t, hostSetMachine)
	session := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_hostset", "tester", &recordingSink{})
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	sv, err = session.Report(sv.Instance, map[string]any{"body": "the draft", "stamp": "first"})
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := session.Instance(sv.Instance)
	before := inst.Store.StateSnapshot()

	_, err = session.Answer(sv.Instance, "review", "adjust", map[string]any{"stamp": "second"}, "keep it")
	if err != nil {
		t.Fatalf("an answer should carry the host-set field beside the option's collect list: %v", err)
	}
	if inst.Store.StateSnapshot() == before {
		t.Error("the host-set field should stay in the confirmation snapshot")
	}
	if _, shown := inst.Store.Collected()["stamp"]; shown {
		t.Error("the resume projection should not hand the host-set field back")
	}
	_, err = session.Answer(sv.Instance, "review", "adjust", map[string]any{"title": "x"}, "keep it")
	if err == nil {
		t.Error("an answer should still refuse a field the option does not collect")
	}
}

func TestHostSet_SpecRefusesItWhereAnAgentWouldSeeIt(t *testing.T) {
	for name, machine := range map[string]string{
		"on a param": "params:\n    stamp: {type: text, optional: true, hostSet: true, desc: x}\nstate:\n    body: {type: text, desc: x}\n" +
			"steps:\n    - id: draft\n      collect: [body]\n      transitions:\n          - when: hasBody\n            to: end(completed)",
		"collected by a step": "state:\n    stamp: {type: text, optional: true, hostSet: true, desc: x}\n" +
			"steps:\n    - id: draft\n      collect: [\"stamp?\"]\n      transitions:\n          - otherwise: end(completed)",
		"collected by an option": "state:\n    stamp: {type: text, optional: true, hostSet: true, desc: x}\n" +
			"steps:\n    - id: draft\n      chooser: user\n      options:\n          - {choice: finish, collect: [\"stamp?\"], to: end(completed)}",
		"played back": "state:\n    stamp: {type: text, optional: true, hostSet: true, desc: x}\n" +
			"steps:\n    - id: draft\n      chooser: user\n      serveDelta: [stamp]\n      options:\n          - {choice: finish, to: end(completed)}",
	} {
		t.Run(name, func(t *testing.T) {
			content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: hostsetproc\n" + machine + "\n---\n\nhost-set procedure\n"
			entry, err := model.ParseEntry("20261110-120000-d-prc-hst.md", content)
			if err != nil {
				t.Fatalf("fixture entry: %v", err)
			}
			_, err = engine.ParseSpec(entry)
			if err == nil || !strings.Contains(err.Error(), "stamp") {
				t.Fatalf("the spec should be refused naming the host-set field, got %v", err)
			}
		})
	}
}
