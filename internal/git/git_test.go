package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// newTestRepo initializes a temp git repo with a committed seed file and a
// local identity, returning its path. HEAD exists so subsequent commits have a
// parent.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "seed.txt")
	runGit(t, dir, "commit", "-q", "-m", "seed")
	return dir
}

// commitFiles returns the file paths recorded by HEAD relative to its parent.
func commitFiles(t *testing.T, dir string) []string {
	t.Helper()
	return strings.Fields(runGit(t, dir, "show", "--name-only", "--pretty=format:", "HEAD"))
}

// TestCommit_ScopesToPathspec is the regression test for the unscoped-commit
// bug (s-tac-tdz): the auto-commit staged only the given paths but then ran
// `git commit -m` with no pathspec, recording the whole index — so any
// pre-staged unrelated work was swept into the CLI's own commit. The fix passes
// `-- <paths>` to `git commit`; this asserts a pre-staged file stays out of the
// resulting commit and remains staged.
func TestCommit_ScopesToPathspec(t *testing.T) {
	dir := newTestRepo(t)

	// Pre-stage unrelated work, as an agent might before invoking the CLI.
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "unrelated.txt")

	// The file the CLI command "touched".
	if err := os.WriteFile(filepath.Join(dir, "touched.txt"), []byte("touched\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)
	if err := (CLI{}).Commit("touch", "touched.txt"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	files := commitFiles(t, dir)
	if !slices.Contains(files, "touched.txt") {
		t.Errorf("HEAD commit missing touched.txt; files = %v", files)
	}
	if slices.Contains(files, "unrelated.txt") {
		t.Errorf("HEAD commit swept in pre-staged unrelated.txt; files = %v", files)
	}

	// The unrelated work must still be staged, untouched by the commit.
	staged := strings.Fields(runGit(t, dir, "diff", "--cached", "--name-only"))
	if !slices.Contains(staged, "unrelated.txt") {
		t.Errorf("unrelated.txt no longer staged after scoped commit; staged = %v", staged)
	}
}

// TestRemovalCommit_ScopesToPathspec covers the deletion path (wip done):
// the same fix must not let a pre-staged index leak into the marker-removal
// commit, and `git commit -- <deleted-path>` must still record the deletion.
func TestRemovalCommit_ScopesToPathspec(t *testing.T) {
	dir := newTestRepo(t)

	// A tracked marker file that the command will remove.
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "marker.txt")
	runGit(t, dir, "commit", "-q", "-m", "add marker")

	// Pre-stage unrelated work.
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "unrelated.txt")

	// FinishWIP removes the marker file from disk before committing.
	if err := os.Remove(filepath.Join(dir, "marker.txt")); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)
	if err := (RemovalCommitter{}).Commit("remove marker", "marker.txt"); err != nil {
		t.Fatalf("RemovalCommitter.Commit: %v", err)
	}

	files := commitFiles(t, dir)
	if !slices.Contains(files, "marker.txt") {
		t.Errorf("HEAD commit did not record marker.txt deletion; files = %v", files)
	}
	if slices.Contains(files, "unrelated.txt") {
		t.Errorf("HEAD commit swept in pre-staged unrelated.txt; files = %v", files)
	}

	// The deletion must be real (marker.txt gone from the tree) and the
	// unrelated work must still be staged.
	tracked := strings.Fields(runGit(t, dir, "ls-files"))
	if slices.Contains(tracked, "marker.txt") {
		t.Errorf("marker.txt still tracked after removal commit; tracked = %v", tracked)
	}
	if !slices.Contains(tracked, "unrelated.txt") {
		t.Errorf("unrelated.txt no longer staged after scoped commit; tracked = %v", tracked)
	}
}

// A graph kept in its own checkout beside the project is committed while the
// process runs in the project, so the commit has to reach a repository the
// working directory is not in.
func TestCommit_TargetsTheConfiguredRepository(t *testing.T) {
	project := newTestRepo(t)
	sidecar := newTestRepo(t)

	entry := filepath.Join(sidecar, "graph", "entry.md")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(project)
	if err := (CLI{Dir: sidecar}).Commit("sdd: capture", entry); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if files := commitFiles(t, sidecar); !slices.Contains(files, "graph/entry.md") {
		t.Errorf("the entry is missing from the sidecar's HEAD; files = %v", files)
	}
	if subject := strings.TrimSpace(runGit(t, project, "log", "-1", "--format=%s")); subject != "seed" {
		t.Errorf("the project repository gained a commit: %q", subject)
	}
}

func TestRemovalCommit_TargetsTheConfiguredRepository(t *testing.T) {
	project := newTestRepo(t)
	sidecar := newTestRepo(t)

	marker := filepath.Join(sidecar, "marker.md")
	if err := os.WriteFile(marker, []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, sidecar, "add", "marker.md")
	runGit(t, sidecar, "commit", "-q", "-m", "add marker")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	t.Chdir(project)
	if err := (RemovalCommitter{Dir: sidecar}).Commit("sdd: wip done", marker); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if files := commitFiles(t, sidecar); !slices.Contains(files, "marker.md") {
		t.Errorf("the removal is missing from the sidecar's HEAD; files = %v", files)
	}
	if subject := strings.TrimSpace(runGit(t, project, "log", "-1", "--format=%s")); subject != "seed" {
		t.Errorf("the project repository gained a commit: %q", subject)
	}
}

// Ambient operations follow the same rule: a branch listing or a status read
// answers for the configured repository, not the process working directory.
func TestAmbientOperationsFollowTheConfiguredRepository(t *testing.T) {
	project := newTestRepo(t)
	sidecar := newTestRepo(t)
	runGit(t, sidecar, "branch", "graph-only")

	t.Chdir(project)
	if !(CLI{Dir: sidecar}).BranchMerged("graph-only") {
		t.Error("BranchMerged must answer for the configured repository")
	}
	if (CLI{}).BranchMerged("graph-only") {
		t.Error("with no directory the answer must come from the working directory")
	}

	if err := os.WriteFile(filepath.Join(sidecar, "dirty.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, err := (CLI{Dir: sidecar}).IsClean(t.Context())
	if err != nil {
		t.Fatalf("IsClean: %v", err)
	}
	if clean {
		t.Error("IsClean must report the configured repository's dirty tree")
	}
}

// RepoRootFor answers for a path the process is not in, and reports no root
// outside a repository rather than guessing one.
func TestRepoRootFor(t *testing.T) {
	sidecar := newTestRepo(t)
	nested := filepath.Join(sidecar, "graph", "2026")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	root := RepoRootFor(nested)
	resolved, err := filepath.EvalSymlinks(sidecar)
	if err != nil {
		resolved = sidecar
	}
	if root != resolved && root != sidecar {
		t.Errorf("RepoRootFor(%s) = %q, want %q", nested, root, resolved)
	}
	if outside := RepoRootFor(t.TempDir()); outside != "" {
		t.Errorf("a path outside a repository must report no root, got %q", outside)
	}
}

// A commit meeting another process's index lock waits for it to clear rather
// than failing: captures running beside a backfill hold the lock briefly.
func TestCommit_WaitsOutAHeldIndexLock(t *testing.T) {
	dir := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "touched.txt"), []byte("touched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(400 * time.Millisecond)
		released <- os.Remove(lock)
	}()

	if err := (CLI{Dir: dir}).Commit("touch", "touched.txt"); err != nil {
		t.Fatalf("a commit should succeed once the index lock clears: %v", err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	if files := commitFiles(t, dir); !slices.Contains(files, "touched.txt") {
		t.Errorf("the commit should record the touched file, got %v", files)
	}
}

// A failure other than the index lock is not retried.
func TestCommit_FailsAtOnceOnOtherErrors(t *testing.T) {
	dir := newTestRepo(t)
	start := time.Now()
	if err := (CLI{Dir: dir}).Commit("nothing", "absent.txt"); err == nil {
		t.Fatal("staging a path that does not exist should fail")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("a failure unrelated to the index lock should return without waiting, took %s", elapsed)
	}
}

func TestModifiedFilesAndHeadContent(t *testing.T) {
	dir := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "gone.txt")
	runGit(t, dir, "commit", "-q", "-m", "gone")
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Dir: dir}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	modified, err := cli.ModifiedFiles(t.Context(), root)
	if err != nil {
		t.Fatalf("ModifiedFiles: %v", err)
	}
	if want := filepath.Join(root, "seed.txt"); len(modified) != 1 || modified[0] != want {
		t.Errorf("only the modified tracked file should be listed, as an absolute path, got %v", modified)
	}

	head, err := cli.HeadContent(t.Context(), filepath.Join(root, "seed.txt"))
	if err != nil {
		t.Fatalf("HeadContent: %v", err)
	}
	if string(head) != "seed\n" {
		t.Errorf("HEAD content should be the committed version, got %q", head)
	}
}
