package repos_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/repos"
)

// Repo IDs may nest, and the walk that lists clones stops at the outer one.
func TestRetireUnconnectedCloneKeepsACloneHoldingAnother(t *testing.T) {
	cases := []struct {
		name   string
		config string
		inner  func(t *testing.T, dir string)
	}{
		{
			name:   "a connected clone inside it",
			config: "repos:\n  - repo_id: git.example.com/foo/bar\n    clone_url: git@git.example.com:foo/bar.git\n",
			inner:  makeClone,
		},
		{
			name:   "a connected repo below it in another case, its .git a file",
			config: "repos:\n  - repo_id: git.example.com/FOO/bar\n    clone_url: git@git.example.com:FOO/bar.git\n",
			inner: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, ".git"), "gitdir: /elsewhere\n")
			},
		},
		{
			name:   "an unlisted clone inside it",
			config: keptRepo,
			inner:  makeClone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc := cloneLocations(t)
			writeFile(t, loc.ConfigPath, tc.config)
			outer := repos.UnconnectedClone{Dir: ownedCloneDir(loc, "git.example.com/foo"), RepoID: "git.example.com/foo", Owned: true}
			makeClone(t, outer.Dir)
			inner := filepath.Join(outer.Dir, "bar")
			tc.inner(t, inner)
			registry := repos.NewRegistry(loc)
			if err := repos.NewManager(registry, nil).MarkDisconnected(outer.RepoID); err != nil {
				t.Fatal(err)
			}
			retired, err := registry.RetireUnconnectedClone(outer, letGo)
			if !errors.Is(err, repos.ErrCloneHoldsClones) {
				t.Errorf("retiring a clone that holds another should be refused, retired to %q, err %v", retired, err)
			}
			for _, dir := range []string{outer.Dir, inner} {
				if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr != nil {
					t.Errorf("both clones should stay in place, stat says %v", statErr)
				}
			}
		})
	}
}
