package engine_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
)

func TestStrainType_NamesTwoEntriesAndARelation(t *testing.T) {
	strains, err := engine.ParseVarType("list<strain>")
	if err != nil {
		t.Fatal(err)
	}
	got, err := strains.ValidateValue([]any{map[string]any{"ids": []any{"20261101-120000-d-tac-aaa", "20261102-120000-d-tac-bbb"}, "relation": "contradicts"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"ids": []any{"20261101-120000-d-tac-aaa", "20261102-120000-d-tac-bbb"}, "relation": "contradicts"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized %v", got)
	}
	for name, value := range map[string]any{
		"one entry":        map[string]any{"ids": []any{"20261101-120000-d-tac-aaa"}, "relation": "contradicts"},
		"the same entry":   map[string]any{"ids": []any{"20261101-120000-d-tac-aaa", "20261101-120000-d-tac-aaa"}, "relation": "duplicates"},
		"a short id":       map[string]any{"ids": []any{"d-tac-aaa", "20261102-120000-d-tac-bbb"}, "relation": "outdates"},
		"unknown relation": map[string]any{"ids": []any{"20261101-120000-d-tac-aaa", "20261102-120000-d-tac-bbb"}, "relation": "overlaps"},
		"unknown key":      map[string]any{"ids": []any{"20261101-120000-d-tac-aaa", "20261102-120000-d-tac-bbb"}, "relation": "outdates", "why": "x"},
	} {
		_, err := strains.ValidateValue([]any{value})
		if err == nil || !strings.Contains(err.Error(), "strain") && !strings.Contains(err.Error(), "relation") {
			t.Errorf("%s: err = %v; want a refusal naming the strain", name, err)
		}
	}
}
