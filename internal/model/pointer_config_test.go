package model_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestTopLevelKeys(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []string
	}{
		{name: "empty file", yaml: "", want: nil},
		{name: "comments only", yaml: "# graph_dir: x\n", want: nil},
		{name: "nested keys stay inside their parent", yaml: "graph_repo: example.test/acme/graph\nllm:\n  provider: x\nskill_scope: project\n", want: []string{"graph_repo", "llm", "skill_scope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := model.TopLevelKeys([]byte(tc.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("top-level keys should list each key once, in file order: got %v", got)
			}
		})
	}
}

func TestDefaultGraphDirFor(t *testing.T) {
	cases := []struct {
		name      string
		graphOnly bool
		want      string
	}{
		{name: "repository holding only the graph", graphOnly: true, want: "graph"},
		{name: "repository holding code and graph", graphOnly: false, want: ".sdd/graph"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.DefaultGraphDirFor(tc.graphOnly); got != tc.want {
				t.Errorf("the default graph directory should fit the layout, got %q", got)
			}
		})
	}
}
