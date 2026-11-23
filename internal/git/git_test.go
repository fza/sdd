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

// git add refuses a pathspec that matches nothing on disk or in the index, which
// a tracked file removed with git rm is; the commit still has to record it.
func TestCommit_RecordsATrackedPathMissingFromDisk(t *testing.T) {
	for _, unstaged := range []bool{false, true} {
		name := "deletion staged"
		if unstaged {
			name = "deletion unstaged"
		}
		t.Run(name, func(t *testing.T) {
			dir := newTestRepo(t)
			if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("* text=auto\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "add", ".gitattributes")
			runGit(t, dir, "commit", "-q", "-m", "add attributes")
			if unstaged {
				if err := os.Remove(filepath.Join(dir, ".gitattributes")); err != nil {
					t.Fatal(err)
				}
			} else {
				runGit(t, dir, "rm", "-q", ".gitattributes")
			}
			if err := os.WriteFile(filepath.Join(dir, "touched.txt"), []byte("touched\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			t.Chdir(dir)
			if err := (CLI{}).Commit("sdd: init", filepath.Join(dir, ".gitattributes"), "touched.txt"); err != nil {
				t.Fatalf("Commit: %v", err)
			}

			files := commitFiles(t, dir)
			if !slices.Contains(files, ".gitattributes") || !slices.Contains(files, "touched.txt") {
				t.Errorf("the commit should record the deletion and the written file; files = %v", files)
			}
			if tracked := strings.Fields(runGit(t, dir, "ls-files")); slices.Contains(tracked, ".gitattributes") {
				t.Errorf("the deleted path should no longer be tracked; tracked = %v", tracked)
			}
		})
	}
}

// A commit naming only deleted paths stages nothing: a bare `git add --all --`
// would stage the whole tree.
func TestCommit_OnlyDeletedPathsStagesNothingElse(t *testing.T) {
	dir := newTestRepo(t)
	runGit(t, dir, "rm", "-q", "seed.txt")
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)
	if err := (CLI{}).Commit("remove seed", "seed.txt"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if files := commitFiles(t, dir); !slices.Equal(files, []string{"seed.txt"}) {
		t.Errorf("the commit should record only the deletion; files = %v", files)
	}
	if tracked := strings.Fields(runGit(t, dir, "ls-files")); slices.Contains(tracked, "unrelated.txt") {
		t.Errorf("an unrelated untracked file should stay out of the index; tracked = %v", tracked)
	}
}

// A path with glob characters names that one file, not every file it matches.
func TestCommit_TakesPathsLiterally(t *testing.T) {
	dir := newTestRepo(t)
	for _, name := range []string{"note*.txt", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("note\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Chdir(dir)
	if err := (CLI{}).Commit("note", "note*.txt"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if files := commitFiles(t, dir); !slices.Equal(files, []string{"note*.txt"}) {
		t.Errorf("the commit should record only the named file; files = %v", files)
	}
	if tracked := strings.Fields(runGit(t, dir, "ls-files")); slices.Contains(tracked, "notes.txt") {
		t.Errorf("a file the name only matches as a glob should stay out of the index; tracked = %v", tracked)
	}
}

// git commit with an empty pathspec records the whole index, so a commit naming
// no path is refused.
func TestCommit_RefusesNoPaths(t *testing.T) {
	dir := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "staged.txt")
	head := runGit(t, dir, "rev-parse", "HEAD")

	t.Chdir(dir)
	if err := (CLI{}).Commit("sdd: nothing"); err == nil {
		t.Error("a commit naming no path should be refused")
	}
	if runGit(t, dir, "rev-parse", "HEAD") != head {
		t.Errorf("a pre-staged file should not land in a commit naming no path; files = %v", commitFiles(t, dir))
	}
}

// A commit meeting another process's index lock waits for it to clear rather
// than failing: concurrent captures hold the lock briefly.
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

	t.Chdir(dir)
	if err := (CLI{}).Commit("touch", "touched.txt"); err != nil {
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
	t.Chdir(dir)
	start := time.Now()
	if err := (CLI{}).Commit("nothing", "absent.txt"); err == nil {
		t.Fatal("staging a path that does not exist should fail")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("a failure unrelated to the index lock should return without waiting, took %s", elapsed)
	}
}

// The pending segments are a channel's JSON Lines files; other files beside
// them stay out of the commit.
func TestPendingSegments_ListTheChannelsJSONLines(t *testing.T) {
	dir := newTestRepo(t)
	events := filepath.Join(dir, ".sdd", "events")
	err := os.MkdirAll(filepath.Join(events, "test"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"test/a.jsonl", "test/notes.txt", "a.jsonl"} {
		err := os.WriteFile(filepath.Join(events, name), []byte("{}\n"), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	pending, err := CLI{Dir: dir}.PendingSegments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pending, []string{".sdd/events/test/a.jsonl"}) {
		t.Fatalf("pending should hold the channel's JSON Lines segment alone, holds %v", pending)
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
	file := filepath.Join(nested, "notes.md")
	if err := os.WriteFile(file, []byte("a file a graph_dir may name\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if root := RepoRootFor(file); root != resolved && root != sidecar {
		t.Errorf("a file should lie in the repository of its folder: RepoRootFor(%s) = %q, want %q", file, root, resolved)
	}
	if outside := RepoRootFor(t.TempDir()); outside != "" {
		t.Errorf("a path outside a repository must report no root, got %q", outside)
	}
}

// git exports GIT_DIR to hooks, so sdd run from one inherits the hook's
// repository; the calls that locate a repository by path must still reach the
// repository at that path.
func TestRepositoryLocatorsIgnoreAnInheritedGitDir(t *testing.T) {
	cases := []struct {
		name   string
		gitDir func(work string) string
	}{
		{name: "absolute", gitDir: func(work string) string { return filepath.Join(work, ".git") }},
		{name: "relative", gitDir: func(string) string { return ".git" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work := newTestRepo(t)
			sidecar := newTestRepo(t)
			entry := filepath.Join(sidecar, "graph", "entry.md")
			if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(entry, []byte("entry\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			fresh := filepath.Join(t.TempDir(), "fresh")
			runGit(t, work, "remote", "add", "origin", "https://example.test/work.git")
			runGit(t, sidecar, "remote", "add", "origin", "https://example.test/sidecar.git")
			runGit(t, sidecar, "checkout", "-q", "-b", "graph-line")
			t.Chdir(work)
			t.Setenv("GIT_DIR", tc.gitDir(work))

			root := RepoRootFor(filepath.Dir(entry))
			commitErr := (CLI{Dir: sidecar}).Commit("sdd: capture", entry)
			initErr := (Repositories{}).Init(fresh)
			branch, branchErr := (Repositories{}).CurrentBranch(sidecar)
			remote := (Repositories{}).RemoteURL(sidecar)
			if err := os.Unsetenv("GIT_DIR"); err != nil {
				t.Fatal(err)
			}

			if branchErr != nil || branch != "graph-line" {
				t.Errorf("CurrentBranch should read the repository at the path, got %q (%v)", branch, branchErr)
			}
			if remote != "https://example.test/sidecar.git" {
				t.Errorf("RemoteURL should read the repository at the path, got %q", remote)
			}

			if resolved, _ := filepath.EvalSymlinks(sidecar); root != resolved && root != sidecar {
				t.Errorf("RepoRootFor should name the repository at the path, got %q", root)
			}
			if commitErr != nil {
				t.Errorf("Commit should reach the configured repository: %v", commitErr)
			}
			if files := commitFiles(t, sidecar); !slices.Contains(files, "graph/entry.md") {
				t.Errorf("the entry should be committed in the configured repository; files = %v", files)
			}
			if subject := strings.TrimSpace(runGit(t, work, "log", "-1", "--format=%s")); subject != "seed" {
				t.Errorf("the hook's repository should gain no commit, HEAD is %q", subject)
			}
			if initErr != nil {
				t.Errorf("Init should create the repository at the path: %v", initErr)
			}
			if _, err := os.Stat(filepath.Join(fresh, ".git")); err != nil {
				t.Errorf("Init should create the repository at the path: %v", err)
			}
		})
	}
}

func TestTrackedListsTheTrackedFilesUnderPathsOfTheRepositoryAtRoot(t *testing.T) {
	dir := newTestRepo(t)
	for _, file := range []string{".sdd/config.local.yaml", ".sdd/hooks/local/mine.sh"} {
		err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(filepath.Join(dir, file), []byte("x\n"), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", "-f", ".sdd/config.local.yaml")
	t.Setenv("GIT_DIR", filepath.Join(newTestRepo(t), ".git"))

	tracked, err := TrackedFiles{}.Tracked(t.Context(), dir, ".sdd/config.local.yaml", ".sdd/hooks/local")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tracked, []string{".sdd/config.local.yaml"}) {
		t.Errorf("only the added file should be tracked, in the repository at root whatever GIT_DIR says, got %v", tracked)
	}
	outside, err := TrackedFiles{}.Tracked(t.Context(), t.TempDir(), "x")
	if err == nil {
		t.Errorf("a folder in no repository cannot say what it tracks, got %v", outside)
	}
}

// The paths match whatever their case, as a case-insensitive file system
// opens them.
func TestTrackedMatchesPathsWhateverTheirCase(t *testing.T) {
	dir := newTestRepo(t)
	err := os.MkdirAll(filepath.Join(dir, ".sdd", "hooks", "LOCAL"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{".sdd/CONFIG.LOCAL.YAML", ".sdd/hooks/LOCAL/evil.sh"} {
		err = os.WriteFile(filepath.Join(dir, file), []byte("x\n"), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "-f", file)
	}
	tracked, err := TrackedFiles{}.Tracked(t.Context(), dir, ".sdd/config.local.yaml", ".sdd/hooks/local")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked) != 2 {
		t.Errorf("tracked files spelled in another case should match, got %v", tracked)
	}
}
