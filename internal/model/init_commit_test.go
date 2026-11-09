package model_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestInitSurfaces(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{"config only", []string{".sdd/config.yaml"}, []string{"config"}},
		{"meta and guard count as config", []string{".sdd/meta.json", model.GraphElsewhereFile}, []string{"config"}},
		{
			"every surface in the fixed order",
			[]string{"AGENTS.md", ".claude/settings.json", ".mcp.json", ".sdd/graph/.gitattributes", ".sdd/config.yaml", ".claude/skills/sdd/SKILL.md"},
			[]string{"skills", "config", "ignore files", "MCP registration", "settings", "other files"},
		},
		{"each surface once", []string{".claude/skills/a/SKILL.md", ".agents/skills/a/SKILL.md", ".gitignore", ".sdd/.gitignore"}, []string{"skills", "ignore files"}},
		{"codex registration", []string{".codex/config.toml"}, []string{"MCP registration"}},
		{"unknown path", []string{"CLAUDE.md"}, []string{"other files"}},
		{"nothing", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.InitSurfaces(tt.paths); !slices.Equal(got, tt.want) {
				t.Errorf("InitSurfaces(%v) = %v, want %v", tt.paths, got, tt.want)
			}
		})
	}
}
