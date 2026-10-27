package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestCountCommitsJudgesTheSubjectOnly(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "seed")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "sdd: capture an entry")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "feat: change code", "-m", "sdd: a body line")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "sdd: summarize an entry")

	cases := []struct {
		name    string
		pattern string
		want    int
	}{
		{name: "graph commits", pattern: "^sdd:", want: 2},
		{name: "every commit", pattern: "", want: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := git.CLI{Dir: dir}.CountCommits(context.Background(), "HEAD~3..HEAD", tc.pattern)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("a commit should count by its subject alone, got %d", got)
			}
		})
	}
}

func TestMergeAbortsAConflictCleanly(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	writeTestFile(t, filepath.Join(dir, "shared.md"), "base\n")
	gitRun(t, dir, "add", "shared.md")
	gitRun(t, dir, "commit", "-q", "-m", "seed")
	gitRun(t, dir, "checkout", "-q", "-b", "theirs")
	writeTestFile(t, filepath.Join(dir, "shared.md"), "theirs\n")
	gitRun(t, dir, "commit", "-q", "-am", "sdd: theirs")
	gitRun(t, dir, "checkout", "-q", "main")
	writeTestFile(t, filepath.Join(dir, "shared.md"), "ours\n")
	gitRun(t, dir, "commit", "-q", "-am", "sdd: ours")

	err := git.CLI{Dir: dir}.Merge(context.Background(), "theirs", "sdd: merge theirs")
	if err == nil {
		t.Fatal("a conflicting merge should fail")
	}
	clean, statusErr := git.CLI{Dir: dir}.IsClean(context.Background())
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if !clean {
		t.Error("a failed merge should leave the working tree clean")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); !os.IsNotExist(statErr) {
		t.Error("a failed merge should leave no merge in progress")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
