package cliapp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/test"
)

// A code repository that held its graph at the default .sdd/graph before it
// named a graph repository keeps that graph's runtime line in its root
// .gitignore; init leaves the file as it is and commits nothing for it.
func TestInitInACodeRepositoryKeepsItsRootGitignore(t *testing.T) {
	test.NewGraphRemote(t, graphCloneRepoID)
	code := filepath.Join(canonicalTemp(t), "code")
	err := os.MkdirAll(code, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, code)
	runApp(t, code, "init", "--graph-repo", "https://example.test/acme/app-sdd.git", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
	ignorePath := filepath.Join(code, ".gitignore")
	rootIgnore := "node_modules/\n\n# sdd\n.sdd/graph/.sdd-runtime/\n"
	writeFile(t, ignorePath, rootIgnore)
	gitIn(t, code, "add", ".gitignore")
	gitIn(t, code, "commit", "-q", "-m", "chore: the ignore file of the former graph")
	before := headSubject(t, code)

	runApp(t, code, "init")

	if ignore := readText(t, ignorePath); ignore != rootIgnore {
		t.Errorf("init should leave the root .gitignore byte for byte:\n%s", ignore)
	}
	if subject := headSubject(t, code); subject != before {
		t.Errorf("init should commit nothing for the ignore file, HEAD is %q", subject)
	}
	if status := gitIn(t, code, "status", "--porcelain"); status != "" {
		t.Errorf("init should leave the work tree clean:\n%s", status)
	}
}
