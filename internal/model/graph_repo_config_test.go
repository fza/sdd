package model_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestParseConfigIgnoresGraphDirBesideGraphRepo(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{name: "graph_repo set", yaml: "graph_repo: example.test/acme/app-sdd\ngraph_dir: elsewhere\n", want: ""},
		{name: "graph_repo unset", yaml: "graph_dir: elsewhere\n", want: "elsewhere"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := model.ParseConfig([]byte(tc.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.GraphDir != tc.want {
				t.Errorf("graph_dir should read as %q, got %q", tc.want, cfg.GraphDir)
			}
		})
	}
}

func TestProjectDirIsAnUnknownKey(t *testing.T) {
	data := []byte("graph_dir: graph\nproject_dir: ../project\n")

	unknown, err := model.UnknownYAMLKeys(data, reflect.TypeFor[model.PerRepoConfig]())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(unknown, []string{"project_dir"}) {
		t.Errorf("project_dir should be reported as unknown, got %v", unknown)
	}
	cfg, err := model.ParseConfig(data)
	if err != nil || cfg.GraphDir != "graph" {
		t.Errorf("a config naming project_dir should load past it: %+v, %v", cfg, err)
	}
}

func TestFormatGraphRepoPointerConfigCarriesOnlyPointerKeys(t *testing.T) {
	written := model.FormatGraphRepoPointerConfig(model.PerRepoConfig{GraphRepo: "example.test/acme/app-sdd", SkillScope: model.ScopeProject, SupportedAgents: []model.AgentTarget{"claude"}})

	keys, err := model.TopLevelKeys([]byte(written))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"graph_repo", "skill_scope", "supported_agents"}; !slices.Equal(keys, want) {
		t.Errorf("the pointer config should set %v, sets %v", want, keys)
	}
	parsed, err := model.ParseConfig([]byte(written))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.GraphRepo != "example.test/acme/app-sdd" || parsed.SkillScope != model.ScopeProject || len(parsed.SupportedAgents) != 1 {
		t.Errorf("the written pointer config should parse back to its values: %+v", parsed)
	}
}
