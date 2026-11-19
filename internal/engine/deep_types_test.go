package engine_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
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
		{"unknown source key", engine.TypeDeepFinding, map[string]any{"verdict": "v", "axis": "evidence", "severity": "low", "sources": []any{map[string]any{"kind": "file", "ref": "a.go", "line": 3}}}, "unknown field"},
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

// A session log an earlier build wrote, whose finding cites its sources in an
// older shape, replays with each source in its object form.
func TestDeepFindingLoggedInOlderShapeReplays(t *testing.T) {
	// listFinding is DeepFinding as it stood before sources became objects.
	type listFinding struct {
		Verdict  string   `json:"verdict"`
		Axis     string   `json:"axis"`
		Severity string   `json:"severity"`
		Sources  []string `json:"sources,omitempty"`
	}
	type stringFinding struct {
		Verdict  string `json:"verdict"`
		Axis     string `json:"axis"`
		Severity string `json:"severity"`
		Sources  string `json:"sources,omitempty"`
	}
	for _, tc := range []struct {
		name   string
		logged any
	}{
		{"list of strings", listFinding{Verdict: "the draft misstates it", Axis: "evidence", Severity: "medium", Sources: []string{"internal/foo.go"}}},
		{"one string", stringFinding{Verdict: "the draft misstates it", Axis: "evidence", Severity: "medium", Sources: "internal/foo.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := historySpec(t, `state:
    findings: {type: list<deep-finding>, desc: the findings}
steps:
    - id: first
      chooser: agent
      options:
          - {choice: go, collect: [findings], to: last}
    - id: last
      chooser: agent
      options:
          - {choice: finish, to: end(completed)}`)
			sink := &recordingSink{}
			session := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_history", "tester", sink)
			sv, err := session.Start(spec, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			finding := map[string]any{"verdict": "the draft misstates it", "axis": "contradicts", "severity": "medium"}
			_, err = session.Answer(sv.Instance, "first", "go", map[string]any{"findings": []any{finding}}, "")
			if err != nil {
				t.Fatal(err)
			}
			var log bytes.Buffer
			for _, ev := range sink.events {
				if ev.Event == engine.EventChooserAnswer {
					ev.Data["fields"] = map[string]any{"findings": []any{tc.logged}}
				}
				line, err := json.Marshal(ev)
				if err != nil {
					t.Fatal(err)
				}
				log.Write(append(line, '\n'))
			}
			events, err := engine.ReadEvents(&log)
			if err != nil {
				t.Fatal(err)
			}

			replayed := replayAgainst(t, spec, events)
			inst, _ := replayed.Instance(sv.Instance)
			findings, _ := inst.Store.Get("findings")
			items, _ := findings.([]any)
			if len(items) != 1 {
				t.Fatalf("the replayed store should hold the logged finding, got %v", findings)
			}
			sources, _ := items[0].(map[string]any)["sources"].([]any)
			if len(sources) != 1 {
				t.Fatalf("the replayed finding should keep its source, got %v", items[0])
			}
			source, _ := sources[0].(map[string]any)
			if source["kind"] != model.DeepSourceFile || source["ref"] != "internal/foo.go" {
				t.Fatalf("the string source should replay as a file source, got %v", sources[0])
			}
		})
	}
}
