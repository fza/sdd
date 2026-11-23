package mcpapp_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// gitCheckout is a repository whose notes.txt has two versions, the newer one
// long enough to need more than one result page.
func gitCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(content string) {
		t.Helper()
		err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	write("first version\n")
	run("add", "notes.txt")
	run("commit", "-q", "-m", "add notes")
	write(strings.Repeat("a long line of the second version\n", 3000))
	run("commit", "-q", "-am", "revise notes")
	return dir
}

func gitServer(t *testing.T, reader sdd.GitReader) testEnv {
	t.Helper()
	return newTestServerRuntime(t, nil, "", "", "", func(options *sdd.ProjectRuntimeOptions) { options.Git = reader })
}

func TestReadGit_ReadsTheCheckoutsHistory(t *testing.T) {
	env := gitServer(t, localadapter.GitHistory{Dir: gitCheckout(t)})
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var file mcpserver.ReadGitResult
	call(t, cs, "read_git", map[string]any{"session": session, "op": "file", "rev": "main~1", "path": "notes.txt"}, &file)
	var log mcpserver.ReadGitResult
	call(t, cs, "read_git", map[string]any{"session": session, "op": "log", "limit": 5}, &log)

	if file.Text != "first version\n" {
		t.Fatalf("a file read should serve the file at the revision, got %q", file.Text)
	}
	if !strings.Contains(log.Text, "\trevise notes") || !strings.Contains(log.Text, "\tadd notes") {
		t.Fatalf("a log read should list the commits, got:\n%s", log.Text)
	}
}

func TestReadGit_PagesAnAnswerPastTheResultBound(t *testing.T) {
	env := gitServer(t, localadapter.GitHistory{Dir: gitCheckout(t)})
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var first mcpserver.ReadGitResult
	call(t, cs, "read_git", map[string]any{"session": session, "op": "file", "rev": "main", "path": "notes.txt"}, &first)
	var second mcpserver.ReadGitResult
	call(t, cs, "read_git", map[string]any{"session": session, "page": 2}, &second)

	if len(first.Text) >= 100000 || !strings.Contains(first.Text, "(page 1 of") {
		t.Fatalf("an answer past one result should come in pages, got %d characters", len(first.Text))
	}
	if !strings.Contains(second.Text, "a long line of the second version") {
		t.Fatalf("page 2 should continue the file, got %q", second.Text[:min(len(second.Text), 200)])
	}
}

func TestReadGit_RefusesARevisionShapedLikeAnOption(t *testing.T) {
	env := gitServer(t, localadapter.GitHistory{Dir: gitCheckout(t)})
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	text := callExpectError(t, cs, "read_git", map[string]any{"session": session, "op": "commit", "rev": "--output=leak"})

	if !strings.Contains(text, `revision "--output=leak" begins with -`) {
		t.Fatalf("the refusal should name the refused revision, got %q", text)
	}
}

func TestReadGit_IsNotAvailableWithoutACheckout(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	text := callExpectError(t, cs, "read_git", map[string]any{"session": session, "op": "log"})

	if !strings.Contains(text, "read_git is not available") {
		t.Fatalf("a project without a checkout should say the read is not available, got %q", text)
	}
}
