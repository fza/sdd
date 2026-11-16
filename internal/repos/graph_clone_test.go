package repos_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/test"
)

func TestGraphClonesEnsure(t *testing.T) {
	cases := []struct {
		name     string
		config   string
		graphDir string
		entry    string
		wantCone []string
	}{
		{name: "graph inside .sdd", config: "graph_dir: .sdd/graph\n", graphDir: ".sdd/graph", entry: ".sdd/graph/2026/11/07-120000-s-tac-abc.md", wantCone: []string{".sdd"}},
		{name: "graph_dir outside .sdd joins the cone", config: "graph_dir: decisions\n", graphDir: "decisions", entry: "decisions/2026/11/07-120000-s-tac-abc.md", wantCone: []string{".sdd", "decisions"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remote := test.NewGraphRemote(t, "example.test/acme/app-sdd")
			remote.Commit(t, "seed", map[string]string{".sdd/config.yaml": tc.config, tc.graphDir + "/.gitattributes": model.GraphAttributesLine + "\n", tc.entry: "---\ntype: signal\n---\nx\n", "src/code.go": "package code\n"})
			locations := repos.Locations{DataRoot: filepath.Join(t.TempDir(), "data")}
			clones := repos.NewGraphClones(locations, git.GraphClones{}, gitgate.New(gitgate.Options{StateDir: t.TempDir(), Limits: func(string) gitgate.Limits { return model.DefaultGitLimits() }}))

			dir, cloned, err := clones.Ensure(context.Background(), remote.RepoID, "https://example.test/acme/app.git")
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			if !cloned || dir != filepath.Join(locations.DataRoot, "repos", "example.test", "acme", "app-sdd") {
				t.Fatalf("the first call should clone under the data root, got %s", dir)
			}
			cone, err := git.GraphClones{}.SparseCone(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cone, tc.wantCone) {
				t.Errorf("the cone should hold .sdd and an outside graph_dir, got %v", cone)
			}
			_, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(tc.entry)))
			if statErr != nil {
				t.Errorf("the graph's entries should be checked out: %v", statErr)
			}
			_, statErr = os.Stat(filepath.Join(dir, "src"))
			if statErr == nil {
				t.Error("code outside the cone should stay out")
			}
			attributes, err := os.ReadFile(filepath.Join(dir, ".git", "info", "attributes"))
			if err != nil {
				t.Fatal(err)
			}
			if string(attributes) != model.ConfigMergeAttributeLine+"\n" {
				t.Errorf("the clone's own attributes should route only the config:\n%s", attributes)
			}
			routed, err := exec.Command("git", "-C", dir, "check-attr", "merge", "--", tc.entry).CombinedOutput()
			if err != nil || !strings.HasSuffix(strings.TrimSpace(string(routed)), "merge: "+model.EntryMergeDriver) {
				t.Errorf("entries should route to the entry merge driver through the graph's .gitattributes: %v\n%s", err, routed)
			}

			_, cloned, err = clones.Ensure(context.Background(), remote.RepoID, "")
			if err != nil || cloned {
				t.Errorf("a second call should keep the clone, not clone again: %v", err)
			}
		})
	}
}
