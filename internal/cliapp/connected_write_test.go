package cliapp_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
	"github.com/networkteam/sdd/test"
)

const (
	homeRepoID     = model.RepoID("example.test/acme/home")
	platformRepoID = model.RepoID("example.test/acme/platform-sdd")
)

// connectedWriteFixture serves a home project that declares and connects a
// platform graph repository by its path, with each side's gate as given.
func connectedWriteFixture(t *testing.T, connectedWrite, inboundAccept bool) (*appFixture, *test.GraphRemote) {
	t.Helper()
	remote := test.NewGraphRemote(t, platformRepoID)
	remote.Commit(t, "seed the platform graph", map[string]string{
		".sdd/config.yaml": "repo_id: " + string(platformRepoID) + "\ngraph_dir: .sdd/graph\ndefault_branch: main\nlanguage: en\ninbound:\n  accept: " + boolText(inboundAccept) + "\nverify:\n  mode: off\n",
		".sdd/graph/.keep": "",
		".gitignore":       ".sdd/tmp/\n.sdd/config.local.yaml\n",
	})
	llm := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: `{"title": "Connected capture", "summary": "A gap captured from the home project.", "findings": []}`})

	home := filepath.Join(canonicalTemp(t), "home")
	err := os.MkdirAll(home, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, home)
	writeFile(t, filepath.Join(home, ".sdd", "config.yaml"), "repo_id: "+string(homeRepoID)+"\ngraph_dir: .sdd/graph\ndefault_branch: main\nlanguage: en\ndependencies: ["+string(platformRepoID)+"]\nverify:\n  mode: off\n")
	writeFile(t, filepath.Join(home, ".sdd", "graph", ".keep"), "")
	writeFile(t, filepath.Join(home, ".gitignore"), ".sdd/tmp/\n.sdd/config.local.yaml\nxdg-*/\n")
	gitIn(t, home, "add", ".sdd", ".gitignore")
	gitIn(t, home, "commit", "-q", "-m", "home project")
	writeFile(t, filepath.Join(home, ".sdd", "config.local.yaml"), "participant: Ada\nllm:\n  provider: openai\n  model: fake\n  endpoint: "+llm.URL+"\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\nconnected:\n  write: "+boolText(connectedWrite)+"\n")
	writeFile(t, filepath.Join(home, "xdg-config", "sdd", "config.yaml"), "repos:\n  - repo_id: "+string(platformRepoID)+"\n    clone_url: "+remote.Bare+"\n")
	runApp(t, home, "repo", "sync")
	return &appFixture{root: home}, remote
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func callTool(t *testing.T, client *mcp.ClientSession, tool string, args map[string]any) (mcpserver.ServeResult, string, bool) {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var serve mcpserver.ServeResult
	_ = json.Unmarshal(encoded, &serve)
	var text strings.Builder
	for _, content := range result.Content {
		textContent, ok := content.(*mcp.TextContent)
		if ok {
			text.WriteString(textContent.Text)
		}
	}
	return serve, text.String() + string(encoded), result.IsError
}

func captureReport() map[string]any {
	return map[string]any{
		"body":        "The platform graph lacks a record of how the home project consumes its API, so platform changes break the home project unnoticed.",
		"entryKind":   "gap",
		"layer":       "tactical",
		"topics":      []string{"testing/connected"},
		"confidence":  "low",
		"widenReport": "searched the platform graph for consumer records; it holds none.",
	}
}

// A capture into a dependency is refused at its start while either gate
// denies it, naming the gate.
func TestConnectedCaptureRefusedAtStart(t *testing.T) {
	cases := []struct {
		name                          string
		connectedWrite, inboundAccept bool
		wantGate                      string
	}{
		{name: "writing side denies", inboundAccept: true, wantGate: "connected.write"},
		{name: "receiving side denies", connectedWrite: true, wantGate: "inbound.accept"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture, remote := connectedWriteFixture(t, tc.connectedWrite, tc.inboundAccept)
			before := remote.Head(t)
			fixture.serve(t, func(client *mcp.ClientSession) {
				opened, served, failed := callTool(t, client, "start_session", map[string]any{})
				if failed {
					t.Fatalf("start_session: %s", served)
				}
				_, served, failed = callTool(t, client, "start_procedure", map[string]any{"session": opened.Session, "canonical": "capture", "project": string(platformRepoID)})
				if !failed || !strings.Contains(served, tc.wantGate) {
					t.Errorf("the start should be refused naming %s:\n%s", tc.wantGate, served)
				}
			})
			if remote.Head(t) != before {
				t.Error("a refused capture should write nothing")
			}
		})
	}
}

// The receiving gate is read from the clone after its due round: an
// inbound.accept withdrawn upstream since the clone last synced still
// refuses. The clone's one remote is a local path, which the git gate always
// admits, so the round is due.
func TestConnectedCaptureRefusedByAWithdrawnAccept(t *testing.T) {
	fixture, remote := connectedWriteFixture(t, true, true)
	remote.Commit(t, "withdraw inbound captures", map[string]string{
		".sdd/config.yaml": "repo_id: " + string(platformRepoID) + "\ngraph_dir: .sdd/graph\ndefault_branch: main\nlanguage: en\ninbound:\n  accept: false\nverify:\n  mode: off\n",
	})
	before := remote.Head(t)
	fixture.serve(t, func(client *mcp.ClientSession) {
		opened, served, failed := callTool(t, client, "start_session", map[string]any{})
		if failed {
			t.Fatalf("start_session: %s", served)
		}
		_, served, failed = callTool(t, client, "start_procedure", map[string]any{"session": opened.Session, "canonical": "capture", "project": string(platformRepoID)})
		if !failed || !strings.Contains(served, "inbound.accept") {
			t.Errorf("the start should be refused by the graph's current inbound.accept:\n%s", served)
		}
	})
	if remote.Head(t) != before {
		t.Error("a refused capture should write nothing")
	}
}

func platformClone() string {
	return filepath.Join(os.Getenv("XDG_DATA_HOME"), "sdd", "repos", filepath.FromSlash(string(platformRepoID)))
}

// With both gates open, the capture writes, commits and pushes through the
// dependency's sdd-owned clone.
func TestConnectedCaptureWritesThroughTheClone(t *testing.T) {
	fixture, remote := connectedWriteFixture(t, true, true)
	before := remote.Head(t)
	fixture.serve(t, func(client *mcp.ClientSession) {
		opened, served, failed := callTool(t, client, "start_session", map[string]any{})
		if failed {
			t.Fatalf("start_session: %s", served)
		}
		serve, served, failed := callTool(t, client, "start_procedure", map[string]any{"session": opened.Session, "canonical": "capture", "project": string(platformRepoID)})
		if failed {
			t.Fatalf("start_procedure: %s", served)
		}
		serve, served, failed = callTool(t, client, "next", map[string]any{"session": opened.Session, "instance": serve.Instance, "report": captureReport()})
		if failed || serve.Step != "playback" {
			t.Fatalf("the capture should reach playback, got step %q:\n%s", serve.Step, served)
		}
		_, served, failed = callTool(t, client, "next", map[string]any{"session": opened.Session, "instance": serve.Instance,
			"report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "yes, capture it"}})
		if failed {
			t.Fatalf("the confirmed capture should write:\n%s", served)
		}
	})
	after := remote.Head(t)
	if after == before {
		t.Fatal("the capture should be pushed to the dependency's graph repository")
	}
	listed := test.Git(t, remote.Bare, "ls-tree", "-r", "--name-only", after, "--", ".sdd/graph")
	if !strings.Contains(listed, "-s-tac-") {
		t.Errorf("the pushed commit should add the gap entry:\n%s", listed)
	}
	if strings.TrimSpace(test.Git(t, platformClone(), "rev-parse", "HEAD")) != after {
		t.Error("the capture should be made in the dependency's sdd-owned clone")
	}
}

// One call syncs a dependency's clone at most once, though the git gate
// admits a local-path remote every time and the confirm resolves the
// dependency for writing several times (20261107-095427-d-tac-yux).
func TestConnectedCaptureConfirmSyncsTheDependencyOnce(t *testing.T) {
	fixture, _ := connectedWriteFixture(t, true, true)
	trace := filepath.Join(t.TempDir(), "git-trace")
	t.Setenv("GIT_TRACE", trace)
	fixture.serve(t, func(client *mcp.ClientSession) {
		session, serve, served, failed := startConnectedCapture(t, client)
		if failed {
			t.Fatalf("start_procedure: %s", served)
		}
		serve, served, failed = callTool(t, client, "next", map[string]any{"session": session, "instance": serve.Instance, "report": captureReport()})
		if failed || serve.Step != "playback" {
			t.Fatalf("the capture should reach playback, got step %q:\n%s", serve.Step, served)
		}
		before := gitFetches(t, trace)
		_, served, failed = callTool(t, client, "next", map[string]any{"session": session, "instance": serve.Instance,
			"report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "yes, capture it"}})
		if failed {
			t.Fatalf("the confirmed capture should write:\n%s", served)
		}
		if rounds := gitFetches(t, trace) - before; rounds != 1 {
			t.Errorf("the confirm should run one sync round of the dependency's clone, ran %d", rounds)
		}
	})
}

// gitFetches counts the fetches git traced to trace: one per sync round.
func gitFetches(t *testing.T, trace string) int {
	t.Helper()
	data, err := os.ReadFile(trace)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "built-in: git fetch")
}

const homeGraphRepoID = model.RepoID("example.test/acme/home-sdd")

// graphRepoHomeFixture serves a code repository whose graph lives in the graph
// repository homeGraphRepoID, which declares the platform graph repository
// as a dependency that accepts inbound captures. localSettings extends the
// code repository's config.local.yaml.
func graphRepoHomeFixture(t *testing.T, localSettings string) (*appFixture, *test.GraphRemote, string) {
	t.Helper()
	homeRemote := test.NewGraphRemote(t, homeGraphRepoID)
	homeRemote.Commit(t, "seed the home graph", map[string]string{
		".sdd/config.yaml": "repo_id: " + string(homeGraphRepoID) + "\ngraph_dir: .sdd/graph\ndefault_branch: main\nlanguage: en\ndependencies: [" + string(platformRepoID) + "]\nverify:\n  mode: off\n",
		".sdd/graph/.keep": "",
		".gitignore":       ".sdd/tmp/\n.sdd/config.local.yaml\n",
	})
	platform := test.NewGraphRemoteBeside(t, homeRemote, platformRepoID)
	platform.Commit(t, "seed the platform graph", map[string]string{
		".sdd/config.yaml": "repo_id: " + string(platformRepoID) + "\ngraph_dir: .sdd/graph\ndefault_branch: main\nlanguage: en\ninbound:\n  accept: true\nverify:\n  mode: off\n",
		".sdd/graph/.keep": "",
		".gitignore":       ".sdd/tmp/\n.sdd/config.local.yaml\n",
	})
	llm := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: `{"title": "Connected capture", "summary": "A gap captured from the home project.", "findings": []}`})

	code := filepath.Join(canonicalTemp(t), "code")
	err := os.MkdirAll(code, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, code)
	writeFile(t, filepath.Join(code, ".sdd", "config.yaml"), "graph_repo: "+string(homeGraphRepoID)+"\ngraph_dir: "+model.GraphElsewhereFile+"\nskill_scope: project\n")
	writeFile(t, filepath.Join(code, model.GraphElsewhereFile), model.GraphElsewhereText(homeGraphRepoID))
	writeFile(t, filepath.Join(code, ".gitignore"), ".sdd/config.local.yaml\nxdg-*/\n")
	gitIn(t, code, "add", ".sdd", ".gitignore")
	gitIn(t, code, "commit", "-q", "-m", "point at the home graph repository")
	writeFile(t, filepath.Join(code, ".sdd", "config.local.yaml"), "participant: Ada\nllm:\n  provider: openai\n  model: fake\n  endpoint: "+llm.URL+"\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n"+localSettings)
	writeFile(t, filepath.Join(code, "xdg-config", "sdd", "config.yaml"), "repos:\n  - repo_id: "+string(platformRepoID)+"\n    clone_url: https://example.test/acme/platform-sdd.git\n")
	runApp(t, code, "repo", "sync")
	homeClone := filepath.Join(os.Getenv("XDG_DATA_HOME"), "sdd", "repos", filepath.FromSlash(string(homeGraphRepoID)))
	return &appFixture{root: code}, platform, homeClone
}

func startConnectedCapture(t *testing.T, client *mcp.ClientSession) (session string, serve mcpserver.ServeResult, served string, failed bool) {
	t.Helper()
	opened, served, failed := callTool(t, client, "start_session", map[string]any{})
	if failed {
		t.Fatalf("start_session: %s", served)
	}
	serve, served, failed = callTool(t, client, "start_procedure", map[string]any{"session": opened.Session, "canonical": "capture", "project": string(platformRepoID)})
	return opened.Session, serve, served, failed
}

// For a project reached through graph_repo, connected.write is read from the
// code repository's config.local.yaml, the checkout the person works in.
func TestGraphRepoConnectedWriteFromTheCodeRepository(t *testing.T) {
	fixture, platform, _ := graphRepoHomeFixture(t, "connected:\n  write: true\n")
	before := platform.Head(t)
	fixture.serve(t, func(client *mcp.ClientSession) {
		session, serve, served, failed := startConnectedCapture(t, client)
		if failed {
			t.Fatalf("the code repository's connected.write should allow the capture:\n%s", served)
		}
		serve, served, failed = callTool(t, client, "next", map[string]any{"session": session, "instance": serve.Instance, "report": captureReport()})
		if failed || serve.Step != "playback" {
			t.Fatalf("the capture should reach playback, got step %q:\n%s", serve.Step, served)
		}
		_, served, failed = callTool(t, client, "next", map[string]any{"session": session, "instance": serve.Instance,
			"report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "yes, capture it"}})
		if failed {
			t.Fatalf("the confirmed capture should write:\n%s", served)
		}
	})
	if platform.Head(t) == before {
		t.Error("the capture should be pushed to the dependency's graph repository")
	}
}

// A connected.write planted in sdd's clone of the home graph never counts.
func TestGraphRepoConnectedWritePlantedInTheCloneIsIgnored(t *testing.T) {
	fixture, platform, homeClone := graphRepoHomeFixture(t, "")
	writeFile(t, filepath.Join(homeClone, ".sdd", "config.local.yaml"), "connected:\n  write: true\n")
	before := platform.Head(t)
	fixture.serve(t, func(client *mcp.ClientSession) {
		_, _, served, failed := startConnectedCapture(t, client)
		if !failed || !strings.Contains(served, "connected.write") {
			t.Errorf("a connected.write in the clone should not allow the capture:\n%s", served)
		}
	})
	if platform.Head(t) != before {
		t.Error("a refused capture should write nothing")
	}
}
