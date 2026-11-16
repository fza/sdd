package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

// A work repository that held its graph at the default .sdd/graph before it
// pointed elsewhere keeps that graph's runtime line in its root .gitignore
// from earlier builds; init removes it and keeps the lines added by hand.
func TestInitInAWorkRepositoryDropsTheRuntimeLineOfItsFormerGraph(t *testing.T) {
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
			writeFile(t, ignorePath, "node_modules/\n\n# sdd\n.sdd/graph/.sdd-runtime/\n")
			gitIn(t, work, "add", ".gitignore")
			gitIn(t, work, "commit", "-q", "-m", "chore: the ignore file of the former graph")

			runApp(t, work, "init")

			ignore := readText(t, ignorePath)
			if strings.Contains(ignore, ".sdd-runtime") {
				t.Errorf("init should drop the runtime line of the graph the work repository held before:\n%s", ignore)
			}
			if !strings.Contains(ignore, "node_modules/") {
				t.Errorf("init should keep the lines added by hand:\n%s", ignore)
			}
			if status := gitIn(t, work, "status", "--porcelain"); status != "" {
				t.Errorf("init should commit the cleaned ignore file:\n%s", status)
			}
		})
	}
}
