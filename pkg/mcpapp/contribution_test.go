package mcpapp_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
	"github.com/networkteam/sdd/test"
)

// contributionServer serves a repository on main whose checkouts are in
// contribution mode for origin/main, with a feature worktree feat/x cut from
// it, through the production repository targets.
func contributionServer(t *testing.T) (*mcpserver.Server, string, string) {
	t.Helper()
	repo := test.GitRepo(t)
	test.Git(t, repo, "branch", "-M", "main")
	write := func(path, content string) {
		t.Helper()
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, ".gitignore"), ".sdd/config.local.yaml\nglobal.yaml\n")
	write(filepath.Join(repo, ".sdd", "config.yaml"), "repo_id: test\ngraph_dir: .sdd/graph\ndefault_branch: main\n")
	write(filepath.Join(repo, ".sdd", "graph", ".gitkeep"), "")
	test.Git(t, repo, "add", ".gitignore", ".sdd/config.yaml", ".sdd/graph/.gitkeep")
	test.Git(t, repo, "commit", "-q", "-m", "sdd project")
	test.Git(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	feature := filepath.Join(resolved, "feature")
	test.Git(t, repo, "worktree", "add", "-q", feature, "-b", "feat/x", "origin/main")
	for _, checkout := range []string{repo, feature} {
		write(filepath.Join(checkout, ".sdd", "config.local.yaml"), "contribution: origin/main\n")
	}
	global := filepath.Join(repo, "global.yaml")
	write(global, "")

	targets, err := localadapter.NewRepositoryTargets("test", repo, global)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{
		Project: "test", GraphDir: filepath.Join(repo, ".sdd", "graph"), Contribution: "origin/main",
		MetaPath: filepath.Join(repo, ".sdd", model.SchemaMetaFileName),
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: "test", DisplayName: "Test"}, DefaultBranch: "main",
		Graph:   localadapter.BranchReadStore{GraphStore: graph, Branches: targets, DefaultBranch: "main"},
		Targets: targets, Branches: targets,
		LLM: pkgllm.RunnerFunc(func(_ context.Context, request pkgllm.Request) (pkgllm.Result, error) {
			if request.Purpose == pkgllm.PurposeSummarize {
				return pkgllm.Result{Text: `{"title": "test title", "summary": "A contributed gap."}`}, nil
			}
			return pkgllm.Result{Text: `{"findings": []}`}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	env := newTestServer(t, nil, "", "", func(options *testServerOptions) {
		options.Application.Access = rootAccess{runtime: runtime}
	})
	return env.srv, repo, feature
}

// confirmContributedCapture runs the capture procedure up to the write and
// returns everything the confirming call served, error or not.
func confirmContributedCapture(t *testing.T, cs *mcp.ClientSession, session string) (*mcp.CallToolResult, string) {
	t.Helper()
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	report := assembleReport()
	delete(report, "refs")
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "report": report}, &serve)
	if serve.Step != "playback" {
		t.Fatalf("expected playback before the write, got %q (%s)", serve.Step, serve.Instructions)
	}
	result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "next", Arguments: map[string]any{
		"session": session, "instance": serve.Instance,
		"report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "yes, capture it"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	structured, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return result, contentText(result) + string(structured)
}

func TestContributionModeOverMCP(t *testing.T) {
	srv, repo, feature := contributionServer(t)
	cs := connect(t, srv)
	session := openSession(t, cs).Session

	before := strings.TrimSpace(test.Git(t, repo, "rev-parse", "main"))
	_, served := confirmContributedCapture(t, cs, session)
	if !strings.Contains(served, "main is the default branch") {
		t.Errorf("a capture on the default branch should be refused and the agent told why:\n%s", served)
	}
	if after := strings.TrimSpace(test.Git(t, repo, "rev-parse", "main")); after != before {
		t.Error("a refused capture should commit nothing on main")
	}

	call(t, cs, "bind_branch", map[string]any{"session": session, "branch": "feat/x"}, &mcpserver.BindBranchResult{})
	result, served := confirmContributedCapture(t, cs, session)
	if result.IsError || strings.Contains(served, "contribution mode refuses") {
		t.Fatalf("a capture on a feature branch cut from the ref should go through:\n%s", served)
	}
	cmd := exec.Command("git", "-C", feature, "log", "-1", "--format=%s")
	subject, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(subject), "sdd:") {
		t.Errorf("the capture should land on feat/x, its tip is %q", subject)
	}
}
