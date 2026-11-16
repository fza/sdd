package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/git"
)

func TestGraphRepoRoot(t *testing.T) {
	repo := newRepo(t)
	graphDir := filepath.Join(repo, ".sdd", "graph")
	if err := os.MkdirAll(graphDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()

	t.Run("existing graph directory", func(t *testing.T) {
		root, err := git.GraphRepoRoot(graphDir)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := filepath.EvalSymlinks(repo)
		got, _ := filepath.EvalSymlinks(root)
		if got != want {
			t.Errorf("the graph directory should resolve to its repository, got %q", root)
		}
	})
	cases := []struct {
		name string
		dir  string
	}{
		{name: "missing graph directory inside a repository", dir: filepath.Join(repo, "missing-graph")},
		{name: "graph directory outside any repository", dir: outside},
		{name: "a file, not a directory", dir: filepath.Join(repo, ".git", "HEAD")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := git.GraphRepoRoot(tc.dir)
			if !errors.Is(err, git.ErrGraphRepoMissing) {
				t.Errorf("a graph directory without its own repository should be reported missing, got %v", err)
			}
		})
	}
}

func TestCommonDirIsSharedByWorktrees(t *testing.T) {
	repo := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-q", "-b", "side", worktree)
	ctx := context.Background()

	main, err := git.CLI{Dir: repo}.CommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := git.CLI{Dir: worktree}.CommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if main != linked || !filepath.IsAbs(main) {
		t.Errorf("both worktrees should name one absolute common git directory, got %q and %q", main, linked)
	}
}

func TestContains(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	cli := git.CLI{Dir: repo}
	seed, err := cli.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "checkout", "-q", "-b", "side")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "side")
	side := gitOut(t, repo, "rev-parse", "HEAD")
	gitRun(t, repo, "checkout", "-q", "main")

	cases := []struct {
		name   string
		commit string
		want   bool
	}{
		{name: "an ancestor", commit: seed, want: true},
		{name: "a commit on another branch", commit: side},
		{name: "an unknown commit", commit: "0123456789abcdef0123456789abcdef01234567"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cli.Contains(ctx, tc.commit)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Error("only a commit reachable from HEAD should count as contained")
			}
		})
	}
}

func TestRevParseRefusesAnUnknownRef(t *testing.T) {
	if _, err := (git.CLI{Dir: newRepo(t)}).RevParse(context.Background(), "origin/none"); err == nil {
		t.Error("an unknown ref should fail to resolve")
	}
}
