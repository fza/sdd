package cliapp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/test"
)

// A work repository that held its graph at the default .sdd/graph before it
// pointed elsewhere keeps that graph's runtime line in its root .gitignore;
// init leaves the file as it is and commits nothing for it.
func TestInitInAWorkRepositoryKeepsItsRootGitignore(t *testing.T) {
	cases := []struct {
		name  string
		setUp func(t *testing.T) string
	}{
		{name: "project_dir", setUp: func(t *testing.T) string {
			work, _ := workRepository(t)
			runApp(t, work, "init", "--project-dir", "../work-sdd", "--graph-dir", "graph", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
			return work
		}},
		{name: "graph_repo", setUp: func(t *testing.T) string {
			test.NewGraphRemote(t, graphCloneRepoID)
			code := filepath.Join(canonicalTemp(t), "code")
			err := os.MkdirAll(code, 0o755)
			if err != nil {
				t.Fatal(err)
			}
			initRepo(t, code)
			runApp(t, code, "init", "--graph-repo", "https://example.test/acme/app-sdd.git", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
			return code
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work := c.setUp(t)
			ignorePath := filepath.Join(work, ".gitignore")
			rootIgnore := "node_modules/\n\n# sdd\n.sdd/graph/.sdd-runtime/\n"
			writeFile(t, ignorePath, rootIgnore)
			gitIn(t, work, "add", ".gitignore")
			gitIn(t, work, "commit", "-q", "-m", "chore: the ignore file of the former graph")
			before := headSubject(t, work)

			runApp(t, work, "init")

			if ignore := readText(t, ignorePath); ignore != rootIgnore {
				t.Errorf("init should leave the root .gitignore byte for byte:\n%s", ignore)
			}
			if subject := headSubject(t, work); subject != before {
				t.Errorf("init should commit nothing for the ignore file, HEAD is %q", subject)
			}
			if status := gitIn(t, work, "status", "--porcelain"); status != "" {
				t.Errorf("init should leave the work tree clean:\n%s", status)
			}
		})
	}
}
