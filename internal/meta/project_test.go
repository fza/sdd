package meta_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
)

func writeConfig(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, ".sdd", name)
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

// Without graph_repo the repository holding .sdd/ is the project, whatever
// else its config names.
func TestResolveProject(t *testing.T) {
	cases := []struct {
		name string
		work map[string]string
	}{
		{
			name: "a project of its own",
			work: map[string]string{"config.yaml": "graph_dir: .sdd/graph\nrepo_id: example.test/work\n"},
		},
		{
			name: "a graph_dir leading into another repository",
			work: map[string]string{"config.yaml": "graph_dir: ../project/graph\n"},
		},
		{
			name: "project_dir in the committed file",
			work: map[string]string{"config.yaml": "project_dir: ../project\nskill_scope: project\nsupported_agents: [claude]\n"},
		},
		{
			name: "project_dir in the local file",
			work: map[string]string{"config.yaml": "graph_dir: .sdd/graph\n", "config.local.yaml": "project_dir: ../project\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			work := filepath.Join(root, "work")
			for name, content := range tc.work {
				writeConfig(t, work, name, content)
			}
			writeConfig(t, filepath.Join(root, "project"), "config.yaml", "graph_dir: graph\n")

			project, err := meta.ResolveProject(work, nil)

			if err != nil {
				t.Fatal(err)
			}
			if project.Root != work || project.WorkRoot != work {
				t.Errorf("the project should be the repository holding .sdd/, got %+v", project)
			}
			if project.Pointed() {
				t.Error("a repository without graph_repo holds its own project")
			}
		})
	}
}

// A repository without a .sdd/ of its own, nested in a project, belongs to
// that project.
func TestDiscoverRootWalksPastARepositoryRoot(t *testing.T) {
	cases := []struct {
		name    string
		gitFile bool
	}{
		{name: "repository root"},
		{name: "linked worktree or submodule root", gitFile: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outer := t.TempDir()
			err := os.Mkdir(filepath.Join(outer, ".sdd"), 0o755)
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(outer, "repo")
			start := filepath.Join(repo, "sub")
			err = os.MkdirAll(start, 0o755)
			if err != nil {
				t.Fatal(err)
			}
			if tc.gitFile {
				err = os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: elsewhere\n"), 0o644)
			} else {
				err = os.Mkdir(filepath.Join(repo, ".git"), 0o755)
			}
			if err != nil {
				t.Fatal(err)
			}

			if root := meta.DiscoverRoot(start); root != outer {
				t.Errorf("discovery should reach the enclosing project %s, found %q", outer, root)
			}

			err = os.Mkdir(filepath.Join(repo, ".sdd"), 0o755)
			if err != nil {
				t.Fatal(err)
			}
			if root := meta.DiscoverRoot(start); root != repo {
				t.Errorf("discovery should find the repository's own .sdd/, found %q", root)
			}
		})
	}
}
