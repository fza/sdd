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
		{name: "nested keys stay inside their parent", yaml: "project_dir: ../p\nllm:\n  provider: x\nskill_scope: project\n", want: []string{"project_dir", "llm", "skill_scope"}},
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

func TestFormatPointerConfigCarriesOnlyPointerKeys(t *testing.T) {
	written := model.FormatPointerConfig(model.PerRepoConfig{ProjectDir: "../work-sdd", SkillScope: model.ScopeProject, SupportedAgents: []model.AgentTarget{"claude"}})

	keys, err := model.TopLevelKeys([]byte(written))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if !slices.Contains(model.PointerKeys, key) {
			t.Errorf("the pointer config should set only pointer keys, sets %s", key)
		}
	}
	parsed, err := model.ParseConfig([]byte(written))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ProjectDir != "../work-sdd" || parsed.SkillScope != model.ScopeProject || len(parsed.SupportedAgents) != 1 {
		t.Errorf("the written pointer config should parse back to its values: %+v", parsed)
	}
}

// A project_dir holding characters YAML gives meaning to reads back unchanged
// from the pointer config FormatPointerConfig writes.
func TestFormatPointerConfigProjectDirReadsBackUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name       string
		projectDir string
	}{
		{"plain path", "../app-sdd"},
		{"colon and space", "../app: sdd"},
		{"hash", "../app #2-sdd"},
		{"double quotes", `"../app-sdd"`},
		{"single quotes", "'../app-sdd'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := model.FormatPointerConfig(model.PerRepoConfig{ProjectDir: tc.projectDir})
			cfg, err := model.ParseConfig([]byte(out))
			if err != nil {
				t.Fatalf("the written pointer config should parse: %v\n%s", err, out)
			}
			if cfg.ProjectDir != tc.projectDir {
				t.Fatalf("project_dir should read back as written, got %q from\n%s", cfg.ProjectDir, out)
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
