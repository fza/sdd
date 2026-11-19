package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

// A code repository that held its graph at the default .sdd/graph before it
// named a graph repository keeps that graph's runtime line in its root
// .gitignore from earlier builds; init removes it and keeps the lines added by
// hand.
func TestInitInACodeRepositoryDropsTheRuntimeLineOfItsFormerGraph(t *testing.T) {
	test.NewGraphRemote(t, graphCloneRepoID)
	code := filepath.Join(canonicalTemp(t), "code")
	err := os.MkdirAll(code, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, code)
	runApp(t, code, "init", "--graph-repo", "https://example.test/acme/app-sdd.git", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
	ignorePath := filepath.Join(code, ".gitignore")
	writeFile(t, ignorePath, "node_modules/\n\n# sdd\n.sdd/graph/.sdd-runtime/\n")
	gitIn(t, code, "add", ".gitignore")
	gitIn(t, code, "commit", "-q", "-m", "chore: the ignore file of the former graph")

	runApp(t, code, "init")

	ignore := readText(t, ignorePath)
	if strings.Contains(ignore, ".sdd-runtime") {
		t.Errorf("init should drop the runtime line of the graph the code repository held before:\n%s", ignore)
	}
	if !strings.Contains(ignore, "node_modules/") {
		t.Errorf("init should keep the lines added by hand:\n%s", ignore)
	}
	if status := gitIn(t, code, "status", "--porcelain"); status != "" {
		t.Errorf("init should commit the cleaned ignore file:\n%s", status)
	}
}
