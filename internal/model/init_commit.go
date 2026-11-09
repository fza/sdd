package model

import (
	"path"
	"strings"
)

// InitSurfaces names the surfaces an `sdd init` re-run changed, from the paths
// it commits (slash-separated, relative to the repository root), in the order
// skills, config, ignore files, MCP registration, settings, other files
// (20261109-234150-d-tac-5lk). A path matching no surface counts as other files.
func InitSurfaces(paths []string) []string {
	order := []string{"skills", "config", "ignore files", "MCP registration", "settings", "other files"}
	found := make(map[string]bool, len(order))
	for _, p := range paths {
		found[initSurface(p)] = true
	}
	var surfaces []string
	for _, surface := range order {
		if found[surface] {
			surfaces = append(surfaces, surface)
		}
	}
	return surfaces
}

func initSurface(p string) string {
	switch {
	case strings.HasPrefix(p, ".claude/skills/"), strings.HasPrefix(p, ".agents/skills/"):
		return "skills"
	case p == SDDDirName+"/config.yaml", p == SDDDirName+"/"+SchemaMetaFileName, p == GraphElsewhereFile:
		return "config"
	case path.Base(p) == ".gitignore", path.Base(p) == ".gitattributes":
		return "ignore files"
	case p == ".mcp.json", p == ".codex/config.toml":
		return "MCP registration"
	case p == ".claude/settings.json":
		return "settings"
	default:
		return "other files"
	}
}
