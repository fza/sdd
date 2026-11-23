package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/git"
)

func TestRepositoriesRootOf(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	err = os.MkdirAll(repo, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "init", "-q", repo).CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	cases := []struct {
		name string
		dir  string
		want string
	}{
		{name: "repository root", dir: repo, want: repo},
		{name: "missing folder inside a repository", dir: filepath.Join(repo, "missing", "deeper"), want: repo},
		{name: "folder outside any repository", dir: filepath.Join(base, "elsewhere"), want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, err := git.Repositories{}.RootOf(tc.dir)
			if err != nil {
				t.Fatalf("finding the root should not fail: %v", err)
			}
			if root != tc.want {
				t.Errorf("the root should be the repository holding the folder, got %q", root)
			}
		})
	}

	t.Run("git failing for another reason", func(t *testing.T) {
		t.Setenv("PATH", "")
		_, err := git.Repositories{}.RootOf(repo)
		if err == nil {
			t.Error("a git failure other than no repository should fail loud, not read as no repository")
		}
	})
}
