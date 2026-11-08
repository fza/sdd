package cliapp_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/cliapp"
	"github.com/networkteam/sdd/internal/model"
)

const strayEntry = "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nparticipants: [Ada]\nsummary: A stray entry.\n---\n\nA stray entry.\n"

// movedRepo is a repository whose graph moved from .sdd/graph to graph in a
// commit carrying the move record.
func movedRepo(t *testing.T) string {
	t.Helper()
	repo := canonicalTemp(t)
	initRepo(t, repo)
	writeFile(t, filepath.Join(repo, ".sdd", "config.yaml"), "graph_dir: graph\ndefault_branch: main\n")
	writeFile(t, filepath.Join(repo, ".sdd", "config.local.yaml"), "participant: Ada\n")
	writeFile(t, filepath.Join(repo, ".gitignore"), ".sdd/config.local.yaml\n")
	writeFile(t, filepath.Join(repo, "graph", "2026", "11", "08-120000-s-tac-kep.md"), strayEntry)
	gitIn(t, repo, "add", ".")
	record := model.MoveRecord{ID: "01JC0000000000000000000000", From: model.GitDirLocation(".sdd/graph"), To: model.GitDirLocation("graph"), Revision: "abc", Entries: 1}
	gitIn(t, repo, "commit", "-q", "-m", "sdd: move graph\n\n"+record.Trailers())
	return repo
}

// appEnv runs the following commands in dir with the XDG directories beside
// it, as runApp does.
func appEnv(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", dataHome(dir))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "xdg-state"))
}

// runAppResult is runApp returning the command's error instead of failing.
func runAppResult(t *testing.T, dir string, args ...string) error {
	t.Helper()
	appEnv(t, dir)
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{Version: "test", Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr})
	return command.Run(t.Context(), append([]string{"sdd"}, args...))
}

// An entry left in a former location of the graph refuses every command,
// naming the file and the move that carries it over.
func TestCommandsRefuseWhileStraysExist(t *testing.T) {
	repo := movedRepo(t)
	if err := runAppResult(t, repo, "show", "20261108-120000-s-tac-kep"); err != nil {
		t.Fatalf("a graph without strays should serve: %v", err)
	}

	stray := filepath.Join(repo, ".sdd", "graph", "2026", "11", "09-090000-d-tac-new.md")
	writeFile(t, stray, strayEntry)
	err := runAppResult(t, repo, "show", "20261108-120000-s-tac-kep")
	if err == nil || !strings.Contains(err.Error(), filepath.ToSlash(stray)) || !strings.Contains(err.Error(), "sdd move --graph-dir graph") {
		t.Errorf("a stray should refuse the command naming the file and the move, got %v", err)
	}

	err = os.Remove(stray)
	if err != nil {
		t.Fatal(err)
	}
	held := filepath.Join(repo, ".sdd", "graph", "2026", "11", "08-120000-s-tac-kep.md")
	writeFile(t, held, strayEntry)
	if err := runAppResult(t, repo, "show", "20261108-120000-s-tac-kep"); err != nil {
		t.Errorf("a copy of an entry the graph holds is no stray: %v", err)
	}
}

// Serve refuses every call while strays exist and serves again once they
// are gone.
func TestServeRefusesCallsWhileStraysExist(t *testing.T) {
	repo := movedRepo(t)
	fixture := &appFixture{root: repo}
	appEnv(t, repo)
	stray := filepath.Join(repo, ".sdd", "graph", "2026", "11", "09-090000-d-tac-new.md")
	fixture.serve(t, func(client *mcp.ClientSession) {
		session, _ := openSession(t, client)
		writeFile(t, stray, strayEntry)
		gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "pull brought a stray")
		_, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if !strings.Contains(failure, filepath.ToSlash(stray)) {
			t.Errorf("a stray arriving with a new commit should refuse the call, got %q", failure)
		}
		err := os.Remove(stray)
		if err != nil {
			t.Fatal(err)
		}
		gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "stray carried")
		_, failure = callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Errorf("serve should answer again once the stray is gone: %s", failure)
		}
	})
}

// A pull bringing an entry into a former location refuses right after it.
func TestSyncRefusesAfterPullingAStray(t *testing.T) {
	repo := movedRepo(t)
	bare := filepath.Join(filepath.Dir(repo), "upstream.git")
	gitIn(t, filepath.Dir(repo), "init", "-q", "--bare", bare)
	gitIn(t, repo, "remote", "add", "origin", bare)
	branch := strings.TrimSpace(gitIn(t, repo, "branch", "--show-current"))
	gitIn(t, repo, "push", "-q", "-u", "origin", branch)
	other := filepath.Join(filepath.Dir(repo), "other")
	gitIn(t, filepath.Dir(repo), "clone", "-q", bare, other)
	gitIn(t, other, "config", "user.email", "test@example.com")
	gitIn(t, other, "config", "user.name", "Test")
	writeFile(t, filepath.Join(other, ".sdd", "graph", "2026", "11", "09-090000-d-tac-new.md"), strayEntry)
	gitIn(t, other, "add", ".sdd/graph")
	gitIn(t, other, "commit", "-q", "-m", "an older build captures in the former folder")
	gitIn(t, other, "push", "-q", "origin", branch)

	err := runAppResult(t, repo, "sync", "--pull")
	if err == nil || !strings.Contains(err.Error(), "sdd move --graph-dir graph") {
		t.Errorf("a pull bringing a stray should end in the refusal, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".sdd", "graph", "2026", "11", "09-090000-d-tac-new.md")); statErr != nil {
		t.Errorf("the pull itself should have run: %v", statErr)
	}
}
