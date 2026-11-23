package cliapp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// contributionFixture is an app fixture whose project is a repository on main
// with an upstream ref origin/main and a feature worktree feat/x cut from it.
func contributionFixture(t *testing.T) (f *appFixture, feature string) {
	t.Helper()
	f = newAppFixture(t)
	git := func(args ...string) { runGit(t, f.root, args...) }
	f.write(t, ".gitignore", "config/\ncache/\nstate/\n.sdd/config.local.yaml\n")
	git("init", "-q", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	git("add", ".")
	git("commit", "-q", "-m", "sdd project")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	worktrees, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	feature = filepath.Join(worktrees, "feature")
	git("worktree", "add", "-q", feature, "-b", "feat/x", "origin/main")
	err = os.WriteFile(filepath.Join(feature, ".sdd", "config.local.yaml"), []byte("participant: Local Author\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return f, feature
}

func captureGap(t *testing.T, f *appFixture) appResult {
	t.Helper()
	return f.run(t, "new", "--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "S.", "s", "tac", "A contributed gap.")
}

func TestContributionModeCapturesOnlyOnTheFeatureBranch(t *testing.T) {
	f, feature := contributionFixture(t)

	t.Chdir(feature)
	if result := f.run(t, "config", "set", "--local", "contribution", "origin/main"); result.err != nil {
		t.Fatalf("config set --local: %v\n%s", result.err, result.stderr)
	}
	if result := captureGap(t, f); result.err != nil {
		t.Fatalf("a capture on a feature branch cut from the ref should go through: %v\n%s", result.err, result.stderr)
	}
	if subject := runGit(t, feature, "log", "-1", "--format=%s"); !strings.HasPrefix(subject, "sdd:") {
		t.Errorf("the capture should land on the feature branch, its tip is %q", subject)
	}
	info := f.run(t, "info")
	if info.err != nil {
		t.Fatal(info.err)
	}
	assertContainsOutput(t, "info", info.stdout, "Mode: contribution (origin/main)")

	t.Chdir(f.root)
	if result := f.run(t, "config", "set", "--local", "contribution", "origin/main"); result.err != nil {
		t.Fatalf("config set --local: %v\n%s", result.err, result.stderr)
	}
	before := runGit(t, f.root, "rev-parse", "HEAD")
	result := captureGap(t, f)
	if result.err == nil || !strings.Contains(result.err.Error()+result.stderr, "main is the default branch") {
		t.Fatalf("a capture on the default branch should be refused naming the cause, got %v\n%s", result.err, result.stderr)
	}
	if runGit(t, f.root, "rev-parse", "HEAD") != before {
		t.Error("a refused capture should commit nothing")
	}
}

func TestContributionModeLeavesOtherCheckoutsAlone(t *testing.T) {
	f, _ := contributionFixture(t)
	if result := captureGap(t, f); result.err != nil {
		t.Fatalf("without the contribution key a capture on the default branch should go through: %v\n%s", result.err, result.stderr)
	}
	if info := f.run(t, "info"); strings.Contains(info.stdout, "Mode: contribution") {
		t.Errorf("info should name no mode outside contribution mode:\n%s", info.stdout)
	}
}

func TestContributionKeyIsRefusedInTheCommittedConfig(t *testing.T) {
	f, _ := contributionFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ncontribution: origin/main\n")
	result := captureGap(t, f)
	if result.err == nil || !strings.Contains(result.err.Error()+result.stderr, "contribution is machine-local") {
		t.Fatalf("a committed contribution key should be refused, got %v\n%s", result.err, result.stderr)
	}
}

func TestContributionModeGuardsEveryGraphWrite(t *testing.T) {
	for _, args := range [][]string{
		{"new", "--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "S.", "s", "tac", "A gap."},
		{"rewrite", "--type", "d", "--kind", "directive", "20260914-020000-s-tac-cli"},
		{"summarize", "--all"},
		{"wip", "start", "20260914-020000-s-tac-cli", "Work in progress."},
		{"wip", "done", "20260914-050000-local-author"},
	} {
		name := args[0]
		if name == "wip" {
			name += " " + args[1]
		}
		t.Run(name, func(t *testing.T) {
			f, _ := contributionFixture(t)
			f.write(t, ".sdd/config.local.yaml", "participant: Local Author\ncontribution: origin/main\n")
			result := f.run(t, args...)
			if result.err == nil || !strings.Contains(result.err.Error()+result.stderr, "main is the default branch") {
				t.Errorf("a graph write on the default branch should be refused in contribution mode, got %v\n%s", result.err, result.stderr)
			}
		})
	}
}

func TestServeReportsContributionMode(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.local.yaml", "participant: Local Author\ncontribution: origin/main\n")
	f.serve(t, func(client *mcp.ClientSession) {
		door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
		if err != nil || door.IsError {
			t.Fatalf("start_session: %v %+v", err, door)
		}
		served, err := json.Marshal(door)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(served), "Mode: contribution (origin/main)") {
			t.Errorf("the session framing should name the mode and the ref:\n%s", served)
		}
		handle, _ := door.StructuredContent.(map[string]any)["session"].(string)
		result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "info", Arguments: map[string]any{"session": handle}})
		if err != nil || result.IsError {
			t.Fatalf("info: %v %+v", err, result)
		}
		if contribution := result.StructuredContent.(map[string]any)["contribution"]; contribution != "origin/main" {
			t.Errorf("MCP info should name the contribution ref, got %v", contribution)
		}
	})
}
