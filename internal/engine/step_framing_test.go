package engine_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// framingMachine is a gate declaring the given framing line, or none when
// framing is empty, followed by a step that declares none.
func framingMachine(framing string) string {
	machine := `state:
    body: {type: text, desc: x}
steps:
    - id: draft
      collect: [body]
`
	if framing != "" {
		machine += "      framing: " + framing + "\n"
	}
	return machine + `      transitions:
          - when: hasBody
            to: close
    - id: close
      chooser: agent
      options:
          - {choice: done, to: end(completed)}`
}

// A step declaring framing blocks serves the names it declares; a step
// declaring none serves nil, which carries every block.
func TestStepFramingServesTheDeclaredNames(t *testing.T) {
	spec := mustParseLanes(t, framingMachine("[info, recentMovement]"), "## unit: draft\n\nDraft.\n")
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	session := eng.NewSession("s_framing", "tester", nopSink{})
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sv.Framing, []string{"info", "recentMovement"}) {
		t.Fatalf("the serve should carry the step's declared framing, got %v", sv.Framing)
	}
	sv, err = session.Report(sv.Instance, map[string]any{"body": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if sv.Step != "close" || sv.Framing != nil {
		t.Fatalf("a step declaring no framing should serve nil framing, at %s with %v", sv.Step, sv.Framing)
	}
}

func TestStepFramingLoadProblems(t *testing.T) {
	for _, tc := range []struct {
		name, framing, problem string
	}{
		{"empty", "[]", "steps.draft: framing names no block"},
		{"twice", "[info, info]", `steps.draft: framing names "info" twice`},
		{"not a name", `["recent movement"]`, `steps.draft: framing names "recent movement", not a block name`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireProblem(t, parseFails(t, framingMachine(tc.framing), "## unit: draft\n\nDraft.\n"), tc.problem)
		})
	}
}
