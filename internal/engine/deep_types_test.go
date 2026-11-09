package engine_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
)

func TestDeepExchangeTypesValidate(t *testing.T) {
	finding := engine.VarType{Base: engine.TypeDeepFinding}
	value, err := finding.ValidateValue(map[string]any{
		"verdict": "the draft says the writer keeps both", "axis": "contradicts", "severity": "high",
		"basis": []any{"20261101-100000-s-tac-abc"}, "edits": []any{map[string]any{"old": "keeps both", "new": "drops the second"}},
	})
	if err != nil {
		t.Fatalf("a well-formed finding validates: %v", err)
	}
	if edits, _ := value.(map[string]any)["edits"].([]any); len(edits) != 1 {
		t.Fatalf("the normalized finding keeps its edits, got %v", value)
	}
	for _, tc := range []struct {
		name  string
		typ   engine.BaseType
		value map[string]any
		want  string
	}{
		{"unknown key", engine.TypeDeepFinding, map[string]any{"verdict": "v", "axis": "contradicts", "severity": "low", "fix": "x"}, "unknown field"},
		{"owner with edits", engine.TypeDeepFinding, map[string]any{"verdict": "v", "axis": "aspiration", "severity": "low", "owner": true, "edits": []any{map[string]any{"old": "a", "new": "b"}}}, "owner finding carries no edits"},
		{"contest without reason", engine.TypeDeepAnswer, map[string]any{"target": "f1", "verdict": "contest"}, "needs the reason"},
		{"unknown outcome", engine.TypeDeepResponse, map[string]any{"target": "f1", "outcome": "ignored", "note": "n"}, `outcome "ignored"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := engine.VarType{Base: tc.typ}.ValidateValue(tc.value)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a refusal naming %q, got %v", tc.want, err)
			}
		})
	}
}

func TestDeepExchangeTypesCarrySchemaAndMeaning(t *testing.T) {
	for _, typ := range []engine.BaseType{engine.TypeDeepFinding, engine.TypeDeepAnswer, engine.TypeDeepResponse} {
		if typ.Description() == "" {
			t.Errorf("%s should carry its served meaning", typ)
		}
	}
}
