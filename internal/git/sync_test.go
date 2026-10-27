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

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "seed")
	return dir
}

func TestPushPublishesOnlyTheCurrentBranch(t *testing.T) {
	dir := newRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, dir, "init", "-q", "--bare", "-b", "main", remote)
	gitRun(t, dir, "remote", "add", "origin", remote)
	gitRun(t, dir, "push", "-q", "-u", "origin", "main")
	gitRun(t, dir, "config", "remote.origin.push", "refs/heads/*:refs/heads/*")
	gitRun(t, dir, "checkout", "-q", "-b", "code")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "feat: unpublished code")
	gitRun(t, dir, "checkout", "-q", "main")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "sdd: capture")

	if err := (git.CLI{Dir: dir}).Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gitOut(t, remote, "rev-parse", "main") != gitOut(t, dir, "rev-parse", "HEAD") {
		t.Error("the push should publish the current branch to its upstream")
	}
	if refs := gitOut(t, remote, "for-each-ref", "--format=%(refname)"); refs != "refs/heads/main" {
		t.Errorf("the push should publish no other branch, got %q", refs)
	}
}

func TestMergeKeepsIgnoredLocalFiles(t *testing.T) {
	dir := newRepo(t)
	writeTestFile(t, filepath.Join(dir, ".gitignore"), "local.yaml\n")
	gitRun(t, dir, "add", ".gitignore")
	gitRun(t, dir, "commit", "-q", "-m", "sdd: ignore local config")
	gitRun(t, dir, "checkout", "-q", "-b", "theirs")
	writeTestFile(t, filepath.Join(dir, "local.yaml"), "theirs\n")
	gitRun(t, dir, "add", "-f", "local.yaml")
	gitRun(t, dir, "commit", "-q", "-m", "sdd: track local config")
	gitRun(t, dir, "checkout", "-q", "main")
	writeTestFile(t, filepath.Join(dir, "local.yaml"), "mine\n")

	if err := (git.CLI{Dir: dir}).Merge(context.Background(), "theirs", "sdd: merge theirs"); err == nil {
		t.Error("a merge that would overwrite an ignored file should fail")
	}
	content, err := os.ReadFile(filepath.Join(dir, "local.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "mine\n" {
		t.Errorf("the ignored local file should keep its content, got %q", content)
	}
}

func TestMergeRefusesACancelledContext(t *testing.T) {
	dir := newRepo(t)
	gitRun(t, dir, "checkout", "-q", "-b", "theirs")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "sdd: theirs")
	gitRun(t, dir, "checkout", "-q", "main")
	before := gitOut(t, dir, "rev-parse", "HEAD")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := (git.CLI{Dir: dir}).Merge(ctx, "theirs", "sdd: merge theirs"); err == nil {
		t.Error("a merge under a cancelled context should not start")
	}
	if gitOut(t, dir, "rev-parse", "HEAD") != before {
		t.Error("a merge that did not start should leave the branch as it was")
	}
}

func TestChangedPathsKeepsNonASCIINames(t *testing.T) {
	dir := newRepo(t)
	gitRun(t, dir, "checkout", "-q", "-b", "theirs")
	writeTestFile(t, filepath.Join(dir, "Übersicht.md"), "x\n")
	gitRun(t, dir, "add", "Übersicht.md")
	gitRun(t, dir, "commit", "-q", "-m", "sdd: add")

	paths, err := git.CLI{Dir: dir}.ChangedPaths(context.Background(), "main", "theirs")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "Übersicht.md" {
		t.Errorf("a non-ASCII path should be listed as written, got %q", paths)
	}
}
