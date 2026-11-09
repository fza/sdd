package cliapp_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

const (
	ownSettings    = "{\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(ls)\"\n    ]\n  }\n}\n"
	ownCodexConfig = "[mcp_servers.other]\ncommand = \"x\"\n"
)

// skippingRepo is a repository whose committed config renders skills for
// Claude Code and Codex under project scope and lists skip under init.skip.
func skippingRepo(t *testing.T, agents []model.AgentTarget, skip string) string {
	t.Helper()
	repo := canonicalTemp(t)
	initRepo(t, repo)
	config := model.FormatConfig(model.PerRepoConfig{GraphDir: model.DefaultGraphDir, Language: "en", SkillScope: model.ScopeProject, SupportedAgents: agents})
	writeFile(t, filepath.Join(repo, ".sdd", "config.yaml"), config+"\ninit:\n  skip: "+skip+"\n")
	gitIn(t, repo, "add", ".sdd/config.yaml")
	gitIn(t, repo, "commit", "-q", "-m", "chore: configure sdd")
	return repo
}

var bothAgents = []model.AgentTarget{model.AgentClaude, model.AgentCodex}

// orphanedSkill is a skill file an earlier build rendered that the bundle no
// longer carries: the render pass deletes it unless skills are skipped.
func orphanedSkill(t *testing.T) string {
	t.Helper()
	entry := model.SkillBundleEntry{Skill: "sdd", RelPath: "references/gone.md", Content: []byte("---\nname: gone\n---\nA reference the bundle dropped.\n")}
	rendered, err := model.RenderSkillFile(entry, model.AgentClaude, "v0.1.0", model.ComputeSkillHash(entry.Content))
	if err != nil {
		t.Fatal(err)
	}
	return string(rendered)
}

// filesUnder maps every file below dir, relative to it, to its content.
func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return filepath.SkipDir
		}
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[rel] = readText(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func headFiles(t *testing.T, repo string) []string {
	t.Helper()
	return strings.Fields(gitIn(t, repo, "show", "--name-only", "--format=", "HEAD"))
}

func TestInitLeavesEverySkippedSurfaceByteIdentical(t *testing.T) {
	repo := skippingRepo(t, bothAgents, "[mcp, permissions, skills]")
	surfaces := map[string]string{
		".mcp.json":                               otherServer,
		".claude/settings.json":                   ownSettings,
		".codex/config.toml":                      ownCodexConfig,
		".claude/skills/sdd/references/gone.md":   orphanedSkill(t),
		".agents/skills/own/SKILL.md":             "---\nname: own\n---\nThe project's own skill.\n",
		".claude/skills/own-skill/SKILL.md":       "---\nname: own-skill\n---\nAnother own skill.\n",
		".claude/skills/sdd/references/kept.json": "{}\n",
	}
	for name, content := range surfaces {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(name)), content)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "chore: the project's own agent setup")
	head := strings.TrimSpace(gitIn(t, repo, "rev-parse", "HEAD"))
	skillsBefore := filesUnder(t, filepath.Join(repo, ".claude", "skills"))
	agentSkillsBefore := filesUnder(t, filepath.Join(repo, ".agents", "skills"))

	runApp(t, repo, "init", "--participant", "Ada")

	for name, content := range surfaces {
		got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(name)))
		if err != nil || string(got) != content {
			t.Errorf("%s should stay byte-identical, got %v:\n%s", name, err, got)
		}
	}
	if got := filesUnder(t, filepath.Join(repo, ".claude", "skills")); !maps.Equal(got, skillsBefore) {
		t.Errorf("the Claude Code skill renders should stay as they were, got %v", keysOf(got))
	}
	if got := filesUnder(t, filepath.Join(repo, ".agents", "skills")); !maps.Equal(got, agentSkillsBefore) {
		t.Errorf("the Codex skill renders should stay as they were, got %v", keysOf(got))
	}
	if strings.TrimSpace(gitIn(t, repo, "rev-parse", "HEAD")) == head {
		t.Fatal("init should still commit what it writes beside the skipped surfaces")
	}
	for _, path := range headFiles(t, repo) {
		for _, surface := range []string{".mcp.json", ".claude/", ".codex/", ".agents/"} {
			if strings.HasPrefix(path, surface) {
				t.Errorf("init's commit should name no skipped surface, got %s", path)
			}
		}
	}
}

func keysOf(files map[string]string) []string {
	return slices.Sorted(maps.Keys(files))
}

func TestInitSkipsOnlyTheListedSurface(t *testing.T) {
	skill := filepath.Join(".claude", "skills", "sdd", "SKILL.md")
	codexSkill := filepath.Join(".agents", "skills", "sdd", "SKILL.md")
	cases := []struct {
		skip    string
		missing []string
		present map[string]string
	}{
		{
			skip:    "mcp",
			missing: []string{".mcp.json", filepath.Join(".codex", "config.toml")},
			present: map[string]string{filepath.Join(".claude", "settings.json"): "mcp__sdd__*", skill: "name: sdd", codexSkill: "name: sdd"},
		},
		{
			skip:    "permissions",
			missing: []string{filepath.Join(".claude", "settings.json")},
			present: map[string]string{".mcp.json": "\"sdd\"", filepath.Join(".codex", "config.toml"): "[mcp_servers.sdd]", skill: "name: sdd", codexSkill: "name: sdd"},
		},
		{
			skip:    "skills",
			missing: []string{filepath.Join(".claude", "skills"), filepath.Join(".agents", "skills")},
			present: map[string]string{".mcp.json": "\"sdd\"", filepath.Join(".codex", "config.toml"): "[mcp_servers.sdd]", filepath.Join(".claude", "settings.json"): "mcp__sdd__*"},
		},
	}
	for _, c := range cases {
		t.Run(c.skip, func(t *testing.T) {
			repo := skippingRepo(t, bothAgents, "["+c.skip+"]")

			runApp(t, repo, "init", "--participant", "Ada")

			for _, path := range c.missing {
				assertMissing(t, filepath.Join(repo, path), "init should leave the skipped surface alone")
			}
			for path, part := range c.present {
				if content := readText(t, filepath.Join(repo, path)); !strings.Contains(content, part) {
					t.Errorf("%s should still get sdd's part %q:\n%s", path, part, content)
				}
			}
		})
	}
}

// Skipping skills also keeps the renders of an agent the selection drops.
func TestInitSkippingSkillsKeepsADroppedAgentsRenders(t *testing.T) {
	repo := skippingRepo(t, bothAgents, "[skills]")
	render := filepath.Join(repo, ".agents", "skills", "sdd", "SKILL.md")
	entry := model.SkillBundleEntry{Skill: "sdd", RelPath: "SKILL.md", Content: []byte("---\nname: sdd\n---\nThe skill.\n")}
	rendered, err := model.RenderSkillFile(entry, model.AgentCodex, "v0.1.0", model.ComputeSkillHash(entry.Content))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, render, string(rendered))
	gitIn(t, repo, "add", ".agents")
	gitIn(t, repo, "commit", "-q", "-m", "chore: the Codex render")

	runApp(t, repo, "init", "--participant", "Ada", "--agents", "claude")

	got, err := os.ReadFile(render)
	if err != nil || string(got) != string(rendered) {
		t.Errorf("a dropped agent's render should stay while skills are skipped, got %v:\n%s", err, got)
	}
	if config := readText(t, filepath.Join(repo, ".sdd", "config.yaml")); !strings.Contains(config, "supported_agents:\n    - claude\n") && !strings.Contains(config, "supported_agents: [claude]") {
		t.Errorf("the agent list should still be recorded:\n%s", config)
	}
}

func TestMoveInstallStepHonorsInitSkip(t *testing.T) {
	cases := []struct {
		skip    string
		missing []string
		present []string
	}{
		{skip: "[mcp, permissions, skills]", missing: []string{".mcp.json", ".claude"}},
		{skip: "[mcp]", missing: []string{".mcp.json"}, present: []string{filepath.Join(".claude", "settings.json"), filepath.Join(".claude", "skills", "sdd", "SKILL.md")}},
		{skip: "[skills]", missing: []string{filepath.Join(".claude", "skills")}, present: []string{".mcp.json", filepath.Join(".claude", "settings.json")}},
	}
	for _, c := range cases {
		t.Run(c.skip, func(t *testing.T) {
			repo := graphRepoToMove(t)
			configPath := filepath.Join(repo, ".sdd", "config.yaml")
			writeFile(t, configPath, readText(t, configPath)+"\ninit:\n  skip: "+c.skip+"\n")
			gitIn(t, repo, "commit", "-q", "-am", "chore: skip agent surfaces")
			project := filepath.Join(filepath.Dir(repo), "graph-home")

			runApp(t, repo, "move", "--project-dir", "../graph-home")

			for _, path := range c.missing {
				assertMissing(t, filepath.Join(project, path), "the install step should leave the skipped surface alone")
			}
			for _, path := range c.present {
				if _, err := os.Stat(filepath.Join(project, path)); err != nil {
					t.Errorf("the install step should still write %s: %v", path, err)
				}
			}
			if pointer := readText(t, configPath); !strings.Contains(pointer, "skip: "+c.skip) {
				t.Errorf("the pointer config should keep init.skip for the agent surfaces left in this checkout:\n%s", pointer)
			}
			runApp(t, repo, "show", movedFirst)
		})
	}
}

func TestConfigSetRefusesAnUnknownInitSkipValue(t *testing.T) {
	repo := skippingRepo(t, bothAgents, "[mcp]")
	for _, args := range [][]string{
		{"config", "set", "--local", "init.skip", "bogus"},
		{"config", "set", "init.skip", "[mcp, bogus]"},
	} {
		_, err := runAppErr(t, repo, args...)
		if err == nil || !strings.Contains(err.Error(), `"bogus" is not mcp, permissions or skills`) {
			t.Errorf("sdd %s should refuse the unknown value, got %v", strings.Join(args, " "), err)
		}
	}
	_, err := runAppErr(t, repo, "config", "set", "--local", "init.skip", "mcp")
	if err == nil || !strings.Contains(err.Error(), "committed .sdd/config.yaml") {
		t.Errorf("init.skip outside the committed config should be refused, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".sdd", "config.local.yaml")); !os.IsNotExist(statErr) {
		t.Errorf("a refused set should write nothing: %v", statErr)
	}
}

func TestAConfigWithAnUnknownInitSkipValueStillLoads(t *testing.T) {
	repo := skippingRepo(t, []model.AgentTarget{model.AgentClaude}, "[mcp, bogus]")

	output, err := runAppErr(t, repo, "config")
	if err != nil {
		t.Fatalf("the config should still load: %v\n%s", err, output)
	}
	if !strings.Contains(output, "init.skip") || !strings.Contains(output, `"bogus" is not mcp, permissions or skills`) {
		t.Errorf("the unknown value should be reported:\n%s", output)
	}

	runApp(t, repo, "init", "--participant", "Ada")
	assertMissing(t, filepath.Join(repo, ".mcp.json"), "the known value should still apply")
	if _, statErr := os.Stat(filepath.Join(repo, ".claude", "settings.json")); statErr != nil {
		t.Errorf("the unknown value should skip nothing: %v", statErr)
	}
}

func TestInitIgnoresInitSkipInTheLocalConfig(t *testing.T) {
	repo := skippingRepo(t, []model.AgentTarget{model.AgentClaude}, "[]")
	writeFile(t, filepath.Join(repo, ".sdd", "config.local.yaml"), "init:\n  skip: [mcp]\n")

	output, err := runAppErr(t, repo, "config")
	if err != nil {
		t.Fatalf("the config should still load: %v\n%s", err, output)
	}
	if !strings.Contains(output, "init.skip") || !strings.Contains(output, "only the committed .sdd/config.yaml may set it") {
		t.Errorf("init.skip in the local config should be reported as ignored:\n%s", output)
	}

	runApp(t, repo, "init", "--participant", "Ada")
	if content := readText(t, filepath.Join(repo, ".mcp.json")); !strings.Contains(content, "\"sdd\"") {
		t.Errorf("init.skip in the local config should not skip the registration:\n%s", content)
	}
}

// A project whose commit-msg hook admits sdd: only under .sdd/ runs init once
// every agent surface is skipped.
func TestInitCommitPassesAHookAdmittingSddOnlyUnderSddDir(t *testing.T) {
	repo := skippingRepo(t, []model.AgentTarget{model.AgentClaude}, "[mcp, permissions, skills]")
	writeFile(t, filepath.Join(repo, ".gitignore"), "node_modules/\nxdg-*/\n")
	writeFile(t, filepath.Join(repo, ".gitattributes"), "*.png binary\n")
	gitIn(t, repo, "add", ".gitignore", ".gitattributes")
	gitIn(t, repo, "commit", "-q", "-m", "chore: the project's own ignore files")
	hooks := canonicalTemp(t)
	hook := filepath.Join(hooks, "commit-msg")
	writeFile(t, hook, "#!/bin/sh\n"+
		"head -n 1 \"$1\" | grep -q '^sdd:' || exit 0\n"+
		"outside=$(git diff --cached --name-only | grep -v '^\\.sdd/')\n"+
		"[ -z \"$outside\" ] && exit 0\n"+
		"echo \"sdd: commits only under .sdd/, got: $outside\" >&2\n"+
		"exit 1\n")
	err := os.Chmod(hook, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "config", "core.hooksPath", hooks)

	runApp(t, repo, "init", "--participant", "Ada")

	if subject := headSubject(t, repo); !strings.HasPrefix(subject, "sdd: ") {
		t.Fatalf("init's commit should have landed, HEAD is %q", subject)
	}
	for _, path := range headFiles(t, repo) {
		if !strings.HasPrefix(path, ".sdd/") {
			t.Errorf("init's commit should hold only paths under .sdd/, got %s", path)
		}
	}
}

// The work repository's init.skip reaches the project its init refreshes, so a
// project whose work repository manages its own agent setup never gets sdd's
// copy beside it (20261109-153425-d-tac-5dh).
func TestInitCarriesTheWorkRepositorysSkipIntoTheProject(t *testing.T) {
	skills := filepath.Join(".claude", "skills")
	settings := filepath.Join(".claude", "settings.json")
	cases := []struct {
		skip    string
		missing []string
		present []string
	}{
		{skip: "[mcp, permissions, skills]", missing: []string{skills, ".mcp.json", settings}},
		{skip: "[skills]", missing: []string{skills}, present: []string{".mcp.json", settings}},
	}
	for _, c := range cases {
		t.Run(c.skip, func(t *testing.T) {
			work, project := workRepository(t)
			runApp(t, work, "init", "--project-dir", "../work-sdd", "--graph-dir", "graph", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
			configPath := filepath.Join(work, ".sdd", "config.yaml")
			writeFile(t, configPath, readText(t, configPath)+"\ninit:\n  skip: "+c.skip+"\n")
			gitIn(t, work, "commit", "-q", "-am", "chore: skip agent surfaces")
			gitIn(t, project, "rm", "-q", "-r", skills, ".mcp.json", settings)
			gitIn(t, project, "commit", "-q", "-m", "chore: drop the agent surfaces")

			runApp(t, work, "init")

			for _, path := range c.missing {
				assertMissing(t, filepath.Join(project, path), "the project refresh should leave a surface the work repository skips alone")
			}
			for _, path := range c.present {
				if _, err := os.Stat(filepath.Join(project, path)); err != nil {
					t.Errorf("the project refresh should still write a surface no repository skips: %v", err)
				}
			}
			if config := readText(t, filepath.Join(project, ".sdd", "config.yaml")); strings.Contains(config, "skip:") {
				t.Errorf("the project's config should not record the work repository's skip:\n%s", config)
			}
		})
	}
}
