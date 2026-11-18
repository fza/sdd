package model_test

import (
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
