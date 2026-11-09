package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestProcedureRoundsRoundTrip(t *testing.T) {
	content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: review\nclass: task\nrounds: 3\n" +
		"steps:\n    - id: only\n      transitions:\n          - otherwise: end(completed)\n---\n\nBody.\n"
	entry, err := model.ParseEntry("20261109-120000-d-prc-rnd.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ProcedureSpec == nil || entry.ProcedureSpec.Rounds != 3 {
		t.Fatalf("the round cap should be retained on the spec, got %+v", entry.ProcedureSpec)
	}
	again, err := model.ParseEntry("20261109-120000-d-prc-rnd.md", model.FormatFrontmatter(entry)+"\n"+entry.Content)
	if err != nil {
		t.Fatal(err)
	}
	if again.ProcedureSpec.Rounds != 3 {
		t.Fatalf("the round cap should survive formatting, got %d", again.ProcedureSpec.Rounds)
	}
}

func TestProcedureSpecFromDocumentRounds(t *testing.T) {
	steps := []any{map[string]any{"id": "only", "transitions": []any{map[string]any{"otherwise": "end(completed)"}}}}
	spec, err := model.ProcedureSpecFromDocument(map[string]any{"steps": steps, "rounds": float64(2)})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Rounds != 2 {
		t.Fatalf("a reported round cap should land on the spec, got %d", spec.Rounds)
	}
	_, err = model.ProcedureSpecFromDocument(map[string]any{"steps": steps, "rounds": -1})
	if err == nil || !strings.Contains(err.Error(), "rounds must be a non-negative integer") {
		t.Fatalf("a negative round cap should be refused by name, got %v", err)
	}
}
