package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

func installFor(t *testing.T, h *handlers.Handler, target model.AgentTarget, root, version string, prompt func(string) (bool, error)) command.SkillInstallResult {
	t.Helper()
	var got command.SkillInstallResult
	err := h.InstallSkills(context.Background(), &command.InstallSkillsCmd{
		Target: target, Scope: model.ScopeProject, RepoRoot: root, BinaryVersion: version,
		PromptOverwrite: prompt,
		OnInstalled:     func(r command.SkillInstallResult) { got = r },
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The delegate's launch configuration installs beside the skills, per
// harness, granting the sdd MCP tools and read-only tools only.
func TestInstallSkills_InstallsTheDelegateAgentDefinition(t *testing.T) {
	cases := []struct {
		target model.AgentTarget
		path   []string
		want   []string
	}{
		{target: model.AgentClaude, path: []string{".claude", "agents", "sdd_delegate.md"}, want: []string{"name: sdd_delegate\n", "tools: mcp__sdd__*, Read, Grep, Glob\n", "sdd-content-hash: "}},
		{target: model.AgentCodex, path: []string{".codex", "agents", "sdd_delegate.toml"}, want: []string{"# sdd-version: v0.2.0\n# sdd-content-hash: ", `name = "sdd_delegate"`, "shell_tool = false"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.target), func(t *testing.T) {
			root := t.TempDir()
			h := handlers.New(handlers.Options{Reader: finders.New(finders.Options{})})
			path := filepath.Join(append([]string{root}, tc.path...)...)

			first := installFor(t, h, tc.target, root, "v0.2.0", nil)

			if !slices.Contains(first.Installed, path) {
				t.Fatalf("init should install the delegate definition, installed %v", first.Installed)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(data), want) {
					t.Fatalf("the installed definition should carry %q:\n%s", want, data)
				}
			}
			if again := installFor(t, h, tc.target, root, "v0.2.0", nil); !slices.Contains(again.Current, path) {
				t.Fatalf("an untouched definition stays current, got %+v", again)
			}
			if bumped := installFor(t, h, tc.target, root, "v0.3.0", nil); !slices.Contains(bumped.Current, path) {
				t.Fatalf("a definition whose content did not change stays current across versions, got %+v", bumped)
			}

			err = os.WriteFile(path, append(data, []byte("\n# a user edit\n")...), 0o644)
			if err != nil {
				t.Fatal(err)
			}
			edited := installFor(t, h, tc.target, root, "v0.2.0", func(string) (bool, error) { return false, nil })
			if !slices.Contains(edited.SkippedModified, path) {
				t.Fatalf("an edited definition is kept unless the user agrees, got %+v", edited)
			}
		})
	}
}
