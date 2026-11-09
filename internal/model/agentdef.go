package model

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

// DelegateAgentName is the name of the subagent definition sdd init installs
// per harness for delegates (20260829-122157-d-cpt-zpe): its tool allowlist
// grants the sdd MCP tools and read-only tools only. Served dispatch
// instructions name it so a harness launches it.
const DelegateAgentName = "sdd_delegate"

// AgentDefinitionDir returns the directory a harness reads subagent
// definitions from: .claude/agents for Claude Code, .codex/agents for Codex,
// under the user's home or the repository root.
func AgentDefinitionDir(target AgentTarget, scope Scope, repoRoot, userHome string) (string, error) {
	var sub []string
	switch target {
	case AgentClaude:
		sub = []string{".claude", "agents"}
	case AgentCodex:
		sub = []string{".codex", "agents"}
	default:
		return "", fmt.Errorf("unsupported agent target: %q", target)
	}
	base := repoRoot
	switch scope {
	case ScopeUser:
		if userHome == "" {
			return "", fmt.Errorf("user home is required for user scope")
		}
		base = userHome
	case ScopeProject:
		if repoRoot == "" {
			return "", fmt.Errorf("repo root is required for project scope")
		}
	default:
		return "", fmt.Errorf("unknown scope: %q", scope)
	}
	return filepath.Join(append([]string{base}, sub...)...), nil
}

// tomlStampPrefix opens the comment lines carrying the install stamps of a
// TOML file, which has no frontmatter to hold them.
const tomlStampPrefix = "# "

// isTOML reports whether a bundle or installed path names a TOML file.
func isTOML(path string) bool { return strings.HasSuffix(path, ".toml") }

// renderTOMLStamped prefixes content with the install stamps as comment lines.
func renderTOMLStamped(content []byte, version, contentHash string) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s%s: %s\n%s%s: %s\n", tomlStampPrefix, SkillStampVersion, version, tomlStampPrefix, SkillStampHash, contentHash)
	buf.Write(content)
	return buf.Bytes()
}

// parseTOMLStamped reads the stamp comment lines off the top of a TOML file,
// returning the stamps and the content below them.
func parseTOMLStamped(data []byte) (version, hash string, content []byte) {
	content = data
	for _, key := range []string{SkillStampVersion, SkillStampHash} {
		line, rest, found := bytes.Cut(content, []byte("\n"))
		value, ok := strings.CutPrefix(string(line), tomlStampPrefix+key+": ")
		if !found || !ok {
			return version, hash, content
		}
		if key == SkillStampVersion {
			version = value
		} else {
			hash = value
		}
		content = rest
	}
	return version, hash, content
}
