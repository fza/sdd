package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

// pullPair returns an upstream repository and a clone tracking it, each
// with one commit of entry.md.
func pullPair(t *testing.T) (upstream, clone string) {
	t.Helper()
	upstream = test.GitRepo(t)
	writeAndCommit(t, upstream, "entry.md", "a\nb\nc\n", "add entry")
	clone = filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "clone", "-q", upstream, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	test.Git(t, clone, "config", "user.email", "test@example.com")
	test.Git(t, clone, "config", "user.name", "Test")
	test.Git(t, clone, "config", "commit.gpgsign", "false")
	return upstream, clone
}

func writeAndCommit(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	test.Git(t, dir, "add", name)
	test.Git(t, dir, "commit", "-q", "-m", message)
}

func headParents(t *testing.T, dir string) int {
	t.Helper()
	return len(strings.Fields(test.Git(t, dir, "rev-list", "--parents", "-n", "1", "HEAD"))) - 1
}

func TestMergePullWritesTheSDDMergeMessage(t *testing.T) {
	upstream, clone := pullPair(t)
	writeAndCommit(t, upstream, "upstream.md", "u\n", "upstream change")
	writeAndCommit(t, clone, "local.md", "l\n", "local change")

	if _, err := (git.CLI{Dir: clone}).MergePull(admitted(t, context.Background())); err != nil {
		t.Fatalf("MergePull: %v", err)
	}

	tracking := strings.TrimSpace(test.Git(t, clone, "rev-parse", "--abbrev-ref", "@{u}"))
	subject := strings.TrimSpace(test.Git(t, clone, "log", "-1", "--format=%s"))
	if want := model.GraphMergeMessage(tracking); subject != want {
		t.Errorf("merge commit subject = %q, want %q", subject, want)
	}
	if got := headParents(t, clone); got != 2 {
		t.Errorf("HEAD should be a merge commit, has %d parent(s)", got)
	}
}

func TestMergePullFastForwardsWithoutAMergeCommit(t *testing.T) {
	upstream, clone := pullPair(t)
	writeAndCommit(t, upstream, "upstream.md", "u\n", "upstream change")

	if _, err := (git.CLI{Dir: clone}).MergePull(admitted(t, context.Background())); err != nil {
		t.Fatalf("MergePull: %v", err)
	}

	want := strings.TrimSpace(test.Git(t, upstream, "rev-parse", "HEAD"))
	if got := strings.TrimSpace(test.Git(t, clone, "rev-parse", "HEAD")); got != want {
		t.Errorf("HEAD = %s, want the upstream commit %s", got, want)
	}
}

func TestMergePullLeavesAConflictToResolve(t *testing.T) {
	upstream, clone := pullPair(t)
	writeAndCommit(t, upstream, "entry.md", "a\nB1\nc\n", "upstream change")
	writeAndCommit(t, clone, "entry.md", "a\nB2\nc\n", "local change")

	if _, err := (git.CLI{Dir: clone}).MergePull(admitted(t, context.Background())); err == nil {
		t.Fatal("MergePull should fail on a conflict")
	}

	if _, err := os.Stat(filepath.Join(clone, ".git", "MERGE_HEAD")); err != nil {
		t.Errorf("the conflicted merge should stay in progress: %v", err)
	}
}

func TestMergePullRefusesABranchWithoutUpstream(t *testing.T) {
	dir := test.GitRepo(t)

	_, err := (git.CLI{Dir: dir}).MergePull(admitted(t, context.Background()))
	if err == nil || !strings.Contains(err.Error(), "no upstream") {
		t.Errorf("MergePull without an upstream = %v, want an error naming the missing upstream", err)
	}
}
