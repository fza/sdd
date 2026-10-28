package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

// contributionRepo holds an upstream ref origin/main one commit ahead of
// branch old, a feature branch cut from the ref, and main at the ref.
func contributionRepo(t *testing.T) string {
	t.Helper()
	dir := test.GitRepo(t)
	test.Git(t, dir, "branch", "-M", "main")
	test.Git(t, dir, "branch", "old")
	err := os.WriteFile(filepath.Join(dir, "upstream.txt"), []byte("upstream\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	test.Git(t, dir, "add", "upstream.txt")
	test.Git(t, dir, "commit", "-q", "-m", "upstream")
	test.Git(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	test.Git(t, dir, "branch", "feat/x", "origin/main")
	return dir
}

func TestCheckContribution(t *testing.T) {
	cases := []struct {
		name          string
		checkout      []string
		ref           string
		defaultBranch string
		cause         string
	}{
		{name: "feature branch cut from the ref", checkout: []string{"feat/x"}, ref: "origin/main", defaultBranch: "main"},
		{name: "default branch", checkout: []string{"main"}, ref: "origin/main", defaultBranch: "main", cause: "main is the default branch"},
		{name: "the ref's branch", checkout: []string{"main"}, ref: "origin/main", defaultBranch: "trunk", cause: "main is the branch of the contribution ref origin/main"},
		{name: "branch not cut from the ref", checkout: []string{"old"}, ref: "origin/main", defaultBranch: "main", cause: "old is not cut from origin/main"},
		{name: "ref not fetched", checkout: []string{"feat/x"}, ref: "origin/missing", defaultBranch: "main", cause: "origin/missing does not resolve"},
		{name: "detached checkout", checkout: []string{"--detach", "feat/x"}, ref: "origin/main", defaultBranch: "main", cause: "no branch checked out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := contributionRepo(t)
			test.Git(t, dir, append([]string{"checkout", "-q"}, tc.checkout...)...)
			err := git.CLI{Dir: dir}.CheckContribution(t.Context(), tc.ref, tc.defaultBranch)
			if tc.cause == "" {
				if err != nil {
					t.Errorf("a feature branch cut from the ref should be allowed, got %v", err)
				}
				return
			}
			if !errors.Is(err, model.ErrContributionRefused) || !strings.Contains(err.Error(), tc.cause) {
				t.Errorf("the refusal should name its cause, got %v", err)
			}
		})
	}
}
