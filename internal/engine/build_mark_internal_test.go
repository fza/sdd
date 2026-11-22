package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// stockEventTypesAtMain is stock's event set as origin/main's engine defines
// it, frozen here so a fork event added to stockEventTypes fails this test.
var stockEventTypesAtMain = []string{
	"abandoned", "chooser_answer", "completed", "labeled", "mutation_intent", "mutation_outcome",
	"op_result", "parked", "read", "reoriented", "report", "served", "served_blocks",
	"session_meta", "started", "transition",
}

// forkOnlyEventTypes are the event types only this build writes. A new event
// type lands here or in stock's set, never in neither.
var forkOnlyEventTypes = []string{
	"anchors", "build_mark", "deferred", "delivered", "fork_ended", "forked", "linked", "model", "mutation_committed", "question", "received", "texts",
}

// definedEventTypes reads every EventType constant the package declares.
func definedEventTypes(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.ValueSpec)
			if !ok || spec.Type == nil {
				return true
			}
			if ident, ok := spec.Type.(*ast.Ident); !ok || ident.Name != "EventType" {
				return true
			}
			for _, value := range spec.Values {
				literal, ok := value.(*ast.BasicLit)
				if !ok {
					t.Fatalf("an EventType constant should be a string literal, got %T", value)
				}
				name, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				types = append(types, name)
			}
			return true
		})
	}
	slices.Sort(types)
	return types
}

func TestStockEventTypesAreStocksAtMain(t *testing.T) {
	var got []string
	for name := range maps.Keys(stockEventTypes) {
		got = append(got, string(name))
	}
	slices.Sort(got)
	if !slices.Equal(got, stockEventTypesAtMain) {
		t.Fatalf("stock's event set should be origin/main's, got %v", got)
	}
}

func TestEveryDefinedEventTypeIsStocksOrForkOnly(t *testing.T) {
	want := slices.Sorted(slices.Values(append(slices.Clone(stockEventTypesAtMain), forkOnlyEventTypes...)))
	if got := definedEventTypes(t); !slices.Equal(got, want) {
		t.Fatalf("the defined event types should be stock's and the listed fork-only ones, got %v, want %v", got, want)
	}
	for _, name := range forkOnlyEventTypes {
		if stockEventTypes[EventType(name)] {
			t.Errorf("fork-only %q should not count as stock's", name)
		}
	}
}
