package model_test

import (
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
