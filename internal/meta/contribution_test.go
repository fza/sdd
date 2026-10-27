package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
)

func TestContributionIsMachineLocal(t *testing.T) {
	cases := []struct {
		name      string
		committed string
		local     string
		want      string
		refused   bool
	}{
		{name: "set in the local layer", committed: "graph_dir: .sdd/graph\n", local: "contribution: origin/main\n", want: "origin/main"},
		{name: "set in the committed layer", committed: "graph_dir: .sdd/graph\ncontribution: origin/main\n", refused: true},
		{name: "not set", committed: "graph_dir: .sdd/graph\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sddDir := t.TempDir()
			writeLayer(t, filepath.Join(sddDir, "config.yaml"), tc.committed)
			if tc.local != "" {
				writeLayer(t, filepath.Join(sddDir, meta.LocalConfigFileName), tc.local)
			}
			cfg, err := meta.ReadConfig(sddDir, "")
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "contribution is machine-local") {
					t.Fatalf("a committed contribution key should be refused, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Contribution != tc.want {
				t.Errorf("the local contribution ref should reach the effective config, got %q", cfg.Contribution)
			}
		})
	}
}

func writeLayer(t *testing.T, path, content string) {
	t.Helper()
	err := os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}
