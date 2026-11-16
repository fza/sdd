package engine_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// A gate collecting a title and a summary, passed only once a check over the
// tag holds and a switch no report reaches is on.
const failingSchemaMachine = `state:
    title: {type: text, desc: the title}
    summary: {type: text, desc: the summary}
    note: {type: text, optional: true, desc: a note}
    tag: {type: text, optional: true, desc: the tag the check reads}
steps:
    - id: draft
      collect: [title, summary, "note?"]
      transitions:
          - when: tagChecked and released
            to: done
    - id: done
      chooser: agent
      options:
          - {choice: finish, to: end(completed)}`

func failingSchemaSession(t *testing.T) (*engine.Session, *engine.Serve, *bool) {
	t.Helper()
	released := false
	registry := engine.NewRegistry()
	for _, p := range []engine.Predicate{
		{
			Doc: engine.FuncDoc{Name: "tagChecked", Doc: "The tag reads ok.", Reads: []string{"tag"}},
			Fn: func(ctx *engine.Context) (bool, error) {
				tag, _ := ctx.Store.Get("tag")
				return tag == "ok", nil
			},
			FailMessage: "the tag is not ok",
		},
		{
			Doc:         engine.FuncDoc{Name: "released", Doc: "A switch outside the store is on."},
			Fn:          func(*engine.Context) (bool, error) { return released, nil },
			FailMessage: "not released",
		},
	} {
		if err := registry.RegisterPredicate(p); err != nil {
			t.Fatal(err)
		}
	}
	spec, err := engine.LoadSpec(delegateEntry(t, "failing", "move", failingSchemaMachine), registry)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)})
	session := eng.NewSession("s_failing", "tester", &recordingSink{})
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return session, sv, &released
}

func schemaFields(t *testing.T, schema map[string]any) []string {
	t.Helper()
	return slices.Sorted(func(yield func(string) bool) {
		for name := range schemaProperties(t, schema) {
			if !yield(name) {
				return
			}
		}
	})
}

// A gate that serves itself again after a report it holds names only the
// fields that hold it in its schema; every other serve carries the whole.
func TestHeldReportServesTheFailingFieldsSchema(t *testing.T) {
	session, sv, released := failingSchemaSession(t)
	full := []string{"note", "summary", "tag", "title"}
	if got := schemaFields(t, sv.ReportSchema); !slices.Equal(got, full) {
		t.Fatalf("the first serve should carry every field, got %v", got)
	}

	sv, err := session.Report(sv.Instance, map[string]any{"title": "a title"})
	if err != nil {
		t.Fatal(err)
	}
	if got := schemaFields(t, sv.ReportSchema); !slices.Equal(got, []string{"summary"}) {
		t.Errorf("a report missing a field should be served the missing field alone, got %v", got)
	}
	if required, _ := sv.ReportSchema["required"].([]string); !slices.Equal(required, []string{"summary"}) {
		t.Errorf("the missing field should stay required, got %v", required)
	}
	if _, closed := sv.ReportSchema["additionalProperties"]; closed {
		t.Error("the failing fields' schema should leave every other field reportable")
	}

	sv, err = session.Report(sv.Instance, map[string]any{"summary": "a summary", "tag": "bad"})
	if err != nil {
		t.Fatal(err)
	}
	if got := schemaFields(t, sv.ReportSchema); !slices.Equal(got, []string{"tag"}) {
		t.Errorf("a held transition should serve the fields its failing checks read, got %v", got)
	}

	sv, err = session.Poll(sv.Instance)
	if err != nil {
		t.Fatal(err)
	}
	if got := schemaFields(t, sv.ReportSchema); !slices.Equal(got, full) {
		t.Errorf("a serve without a report should carry every field, got %v", got)
	}

	sv, err = session.Report(sv.Instance, map[string]any{"tag": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if got := schemaFields(t, sv.ReportSchema); !slices.Equal(got, full) {
		t.Errorf("a gate held only by checks that read no field should serve every field, got %v", got)
	}

	*released = true
	sv, err = session.Poll(sv.Instance)
	if err != nil {
		t.Fatal(err)
	}
	if sv.Step != "done" {
		t.Fatalf("the gate should pass once released, got %s", sv.Step)
	}
}
