package cliapp_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// contributionProject is a repository on main with an upstream ref
// origin/main and a feature worktree feat/x cut from it.
func contributionProject(t *testing.T) (project, feature string) {
	t.Helper()
	root := canonicalTemp(t)
	project = filepath.Join(root, "project")
	feature = filepath.Join(root, "feature")
	writeFile(t, filepath.Join(project, ".gitignore"), ".sdd/config.local.yaml\nxdg-*/\n")
	initRepo(t, project)
	gitIn(t, project, "branch", "-M", "main")
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "repo_id: example.test/contrib\ngraph_dir: .sdd/graph\ndefault_branch: main\n")
	writeFile(t, filepath.Join(project, ".sdd", "graph", ".gitkeep"), "")
	gitIn(t, project, "add", ".gitignore", ".sdd/config.yaml", ".sdd/graph/.gitkeep")
	gitIn(t, project, "commit", "-q", "-m", "sdd project")
	gitIn(t, project, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitIn(t, project, "worktree", "add", "-q", feature, "-b", "feat/x", "origin/main")
	for _, checkout := range []string{project, feature} {
		writeFile(t, filepath.Join(checkout, ".sdd", "config.local.yaml"), "participant: Ada\n")
	}
	return project, feature
}

func captureGap(t *testing.T, dir string) (string, error) {
	t.Helper()
	return runReleased(t, dir, "", "new", "s", "tac", "--confidence", "high", "--skip-preflight", "--summary", "S.", "--title", "T", "A contributed gap.")
}

func TestContributionModeCapturesOnlyOnTheFeatureBranch(t *testing.T) {
	project, feature := contributionProject(t)

	runApp(t, feature, "config", "set", "--local", "contribution", "origin/main")
	if local := readFile(t, filepath.Join(feature, ".sdd", "config.local.yaml")); !strings.Contains(local, "contribution: origin/main") {
		t.Fatalf("config set --local should record the contribution ref:\n%s", local)
	}
	output, err := captureGap(t, feature)
	if err != nil {
		t.Fatalf("a capture on a feature branch cut from the ref should go through: %v\n%s", err, output)
	}
	if subject := headSubject(t, feature); !strings.HasPrefix(subject, "sdd:") {
		t.Errorf("the capture should land on the feature branch, its HEAD is %q", subject)
	}
	info, err := runReleased(t, feature, "", "info")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info, "Mode: contribution (origin/main)") {
		t.Errorf("info should name the mode and the ref:\n%s", info)
	}

	runApp(t, project, "config", "set", "--local", "contribution", "origin/main")
	before := headSubject(t, project)
	output, err = captureGap(t, project)
	if err == nil || !strings.Contains(err.Error()+output, "main is the default branch") {
		t.Fatalf("a capture on the default branch should be refused naming the cause, got %v\n%s", err, output)
	}
	if headSubject(t, project) != before {
		t.Error("a refused capture should commit nothing")
	}
}

func TestContributionModeLeavesOtherCheckoutsAlone(t *testing.T) {
	project, _ := contributionProject(t)
	output, err := captureGap(t, project)
	if err != nil {
		t.Fatalf("without the contribution key a capture on the default branch should go through: %v\n%s", err, output)
	}
}

func TestContributionKeyIsRefusedInTheCommittedConfig(t *testing.T) {
	project, _ := contributionProject(t)
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "repo_id: example.test/contrib\ngraph_dir: .sdd/graph\ndefault_branch: main\ncontribution: origin/main\n")
	output, err := captureGap(t, project)
	if err == nil || !strings.Contains(err.Error()+output, "contribution is machine-local") {
		t.Fatalf("a committed contribution key should be refused, got %v\n%s", err, output)
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

func TestContributionModeGuardsEveryGraphWrite(t *testing.T) {
	for _, args := range [][]string{
		{"new", "s", "tac", "--confidence", "high", "--skip-preflight", "--summary", "S.", "--title", "T", "A gap."},
		{"rewrite", "--type", "d", "--kind", "directive", "20260101-120000-s-tac-abc"},
		{"summarize", "--all"},
		{"wip", "start", "--exclusive", "Work in progress."},
		{"wip", "done", "20260101-120000-ada"},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			project, _ := contributionProject(t)
			writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\ncontribution: origin/main\n")
			output, err := runReleased(t, project, "", args...)
			if err == nil || !strings.Contains(err.Error()+output, "main is the default branch") {
				t.Errorf("a graph write on the default branch should be refused in contribution mode, got %v\n%s", err, output)
			}
		})
	}
}
