package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// A graph_dir holding characters YAML gives meaning to reads back unchanged
// from the config FormatConfig writes.
func TestFormatConfigGraphDirReadsBackUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name     string
		graphDir string
	}{
		{"plain path", ".sdd/graph"},
		{"colon and space", "graphs: main"},
		{"hash", "graph #1"},
		{"leading dash", "- graph"},
		{"double quotes", `"graph"`},
		{"single quotes", "'graph'"},
		{"flow sequence", "[graph]"},
		{"flow mapping", "{graph}"},
		{"spaces", "my graph dir"},
		{"leading and trailing spaces", " graph "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := model.FormatConfig(model.PerRepoConfig{GraphDir: tc.graphDir})
			cfg, err := model.ParseConfig([]byte(out))
			if err != nil {
				t.Fatalf("the written config should parse: %v\n%s", err, out)
			}
			if cfg.GraphDir != tc.graphDir {
				t.Fatalf("graph_dir should read back as written, got %q from\n%s", cfg.GraphDir, out)
			}
		})
	}
}
