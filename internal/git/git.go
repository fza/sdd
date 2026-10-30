// Package git is the production git adapter for the sdd binary: every git
// subprocess sdd runs is implemented here, behind the narrow interfaces its
// consumers define (handlers.Committer/Brancher/Mover/Puller/ChangeReader,
// finders.GitSyncer, the repos package's clone/pull surface). Interfaces stay
// with their consumers; this package only provides the implementations, so a
// future — possibly partial — swap to an in-process implementation such as
// go-git touches nothing but this package.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// CLI is the exec-based adapter: a struct whose methods shell out to the git
// binary. One value satisfies the consumer-defined git interfaces
// (handlers.Committer/Brancher/Mover/Puller/ChangeReader, finders.GitSyncer,
// repos.Git);
// the one exception is the staged-deletion commit variant, which shares the
// Commit method name and therefore lives on its own type (RemovalCommitter).
type CLI struct {
	// Dir is the repository the ambient operations run in. Empty means the
	// process working directory. A graph kept in its own checkout, beside the
	// project rather than inside it, needs entry commits to reach that
	// checkout while the process runs in the project: the working directory
	// alone cannot express both.
	Dir string
}

// at prefixes args with -C Dir so the operation targets this adapter's
// repository. Methods taking an explicit directory (Clone, PullFFOnly) address
// their target through arguments and never route through here.
func (c CLI) at(args ...string) []string {
	if c.Dir == "" {
		return args
	}
	return append([]string{"-C", c.Dir}, args...)
}

// detachedTimeout caps a detached auto-commit or push. A signing or
// credential helper blocking on input would otherwise hang indefinitely when
// sdd runs as a long-lived server; the timeout is a backstop on top of the
// TTY detach in runDetached. See d-tac-zhp.
const detachedTimeout = 30 * time.Second

// Commit stages exactly the given paths and commits them, detached from any
// controlling terminal. It is the production handlers.Committer for the
// auto-commit paths (sdd new, summarize, init, ...).
func (c CLI) Commit(message string, paths ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), detachedTimeout)
	defer cancel()

	addArgs := c.at(append([]string{"add", "--all", "--"}, paths...)...)
	if out, err := runDetachedPastIndexLock(ctx, addArgs...); err != nil {
		return fmt.Errorf("git add: %s (%w)", out, err)
	}

	// Scope the commit to exactly the staged paths with an explicit pathspec.
	// Without `-- <paths>`, `git commit` records the whole index, sweeping any
	// pre-staged unrelated work into the CLI's own commit.
	commitArgs := c.at(append([]string{"commit", "-m", message, "--"}, paths...)...)
	if out, err := runDetachedPastIndexLock(ctx, commitArgs...); err != nil {
		return fmt.Errorf("git commit: %s (%w)", out, err)
	}

	// After the commit, so a config that cannot be written never keeps an
	// entry from being recorded.
	return c.ensureMergeDriverOnce(ctx)
}

// Committable returns paths without those git refuses to stage: untracked
// paths an ignore rule matches, and untracked paths no longer on disk.
func (c CLI) Committable(ctx context.Context, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	ignored, err := c.ignored(ctx, paths)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if ignored[path] {
			continue
		}
		_, statErr := os.Stat(path)
		if errors.Is(statErr, fs.ErrNotExist) {
			tracked, err := c.tracked(ctx, path)
			if err != nil {
				return nil, err
			}
			if !tracked {
				continue
			}
		}
		out = append(out, path)
	}
	return out, nil
}

func (c CLI) ignored(ctx context.Context, paths []string) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "git", c.at("check-ignore", "-z", "--stdin")...)
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("git check-ignore: %s (%w)", stderr.Bytes(), err)
	}
	ignored := map[string]bool{}
	for path := range strings.SplitSeq(string(out), "\x00") {
		if path != "" {
			ignored[path] = true
		}
	}
	return ignored, nil
}

func (c CLI) tracked(ctx context.Context, path string) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("ls-files", "-z", "--", path)...).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git ls-files: %s (%w)", out, err)
	}
	return len(out) > 0, nil
}

// runDetachedPastIndexLock retries a git subprocess while another process
// holds the repository's index lock, which clears when that process finishes;
// a graph written beside concurrent captures meets it routinely. Retries stop
// with ctx, so the commit timeout bounds the wait. Any other failure returns
// at once.
func runDetachedPastIndexLock(ctx context.Context, args ...string) ([]byte, error) {
	delay := 100 * time.Millisecond
	for {
		out, err := runDetached(ctx, args...)
		if err == nil || !strings.Contains(string(out), "index.lock") {
			return out, err
		}
		select {
		case <-ctx.Done():
			return out, err
		case <-time.After(delay):
		}
		delay = min(2*delay, 2*time.Second)
	}
}

// Push publishes HEAD to the branch the current branch tracks, detached like
// Commit. The explicit refspec keeps a configured push refspec or mirror
// setting from publishing other branches the push rule never counted.
// Production handlers.Pusher.
func (c CLI) Push(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, detachedTimeout)
	defer cancel()
	branch, err := c.output(ctx, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("resolving the current branch: %w", err)
	}
	remote, err := c.output(ctx, "config", "branch."+branch+".remote")
	if err != nil {
		return fmt.Errorf("resolving the remote of %s: %w", branch, err)
	}
	merge, err := c.output(ctx, "config", "branch."+branch+".merge")
	if err != nil {
		return fmt.Errorf("resolving the upstream branch of %s: %w", branch, err)
	}
	out, err := runDetached(ctx, c.at("push", remote, "HEAD:"+merge)...)
	if err != nil {
		return fmt.Errorf("git push: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (c CLI) output(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", c.at(args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Merge merges ref into the current branch, committing a merge with message
// when the histories diverged. A failed merge is aborted so the working tree
// stays as it was. Production handlers.GraphPuller.
func (c CLI) Merge(ctx context.Context, ref, message string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("git merge %s: %w", ref, err)
	}
	// A merge killed halfway leaves a partly checked-out tree and a stale
	// index.lock, so once started it outlives the caller's cancellation.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedTimeout)
	defer cancel()
	if err := c.ensureMergeDriverOnce(ctx); err != nil {
		return err
	}
	out, err := runDetached(ctx, c.at("merge", "--no-edit", "--no-overwrite-ignore", "-m", message, ref)...)
	if err == nil {
		return nil
	}
	mergeErr := fmt.Errorf("git merge %s: %s (%w)", ref, strings.TrimSpace(string(out)), err)
	abortOut, abortErr := exec.Command("git", c.at("merge", "--abort")...).CombinedOutput()
	if abortErr != nil && strings.Contains(string(abortOut), "MERGE_HEAD missing") {
		return mergeErr
	}
	if abortErr != nil {
		return errors.Join(mergeErr, fmt.Errorf("git merge --abort: %s (%w)", strings.TrimSpace(string(abortOut)), abortErr))
	}
	return mergeErr
}

// HasCommitMessage reports whether any reachable commit contains text. It is
// used by retryable post-apply finalizers to recognize a commit that landed
// before its durable finalizer outcome could be recorded.
func (c CLI) HasCommitMessage(ctx context.Context, text string) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("log", "--all", "--fixed-strings", "--grep="+text, "--format=%H", "-n", "1")...).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git log: %s (%w)", out, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// RemovalCommitter is the handlers.Committer variant for paths already
// removed from disk: it stages the deletions (`git rm --cached`, falling back
// to `git add`) before committing. Used by FinishWIP, where the marker file
// is gone by the time the commit runs.
type RemovalCommitter struct {
	// Dir is the repository the removal commit runs in, with the same meaning
	// as CLI.Dir.
	Dir string
}

// Commit stages the deletion of the given paths and commits, scoped to those
// paths so an unrelated staged index isn't swept into the removal commit.
func (r RemovalCommitter) Commit(message string, paths ...string) error {
	at := CLI(r).at
	for _, p := range paths {
		rm := exec.Command("git", at("rm", "--cached", "-f", p)...)
		if out, err := rm.CombinedOutput(); err != nil {
			add := exec.Command("git", at("add", p)...)
			if out2, err2 := add.CombinedOutput(); err2 != nil {
				return fmt.Errorf("git stage: %s (%v); fallback %s (%w)", out, err, out2, err2)
			}
		}
	}
	commitArgs := at(append([]string{"commit", "-m", message, "--"}, paths...)...)
	commit := exec.Command("git", commitArgs...)
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s (%w)", out, err)
	}
	return CLI(r).ensureMergeDriverOnce(context.Background())
}

// runDetached runs a git subprocess detached from any controlling terminal
// (Setsid starts a new session with no controlling TTY) and bounded by ctx.
// Detaching is the fix for the auto-commit hang (d-tac-zhp): an interactive
// signing or credential prompt has no TTY to read from, so it fails immediately
// instead of suspending the backgrounded process on SIGTTIN. The context
// timeout is a backstop for any other blocking helper.
func runDetached(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("git %s: timed out after %s (a commit signer or credential helper may be blocking on input)", args[0], detachedTimeout)
	}
	return out, err
}

// ModifiedFiles lists tracked files under dir whose working copy differs from
// HEAD, as absolute paths. Production handlers.ChangeReader.
func (c CLI) ModifiedFiles(ctx context.Context, dir string) ([]string, error) {
	root, err := c.toplevel(ctx)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, fmt.Errorf("locating %s in %s: %w", dir, root, err)
	}
	out, err := exec.CommandContext(ctx, "git", c.at("status", "--porcelain=v1", "-z", "--untracked-files=no", "--", rel)...).Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	var paths []string
	records := strings.Split(string(out), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 {
			continue
		}
		status, path := record[:2], record[3:]
		if status[0] == 'R' || status[0] == 'C' {
			i++ // the source path follows as its own record
			continue
		}
		if status[0] == 'M' || status[1] == 'M' {
			paths = append(paths, filepath.Join(root, path))
		}
	}
	return paths, nil
}

// HeadContent returns the file at path as HEAD records it. Production
// handlers.ChangeReader.
func (c CLI) HeadContent(ctx context.Context, path string) ([]byte, error) {
	root, err := c.toplevel(ctx)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, fmt.Errorf("locating %s in %s: %w", path, root, err)
	}
	out, err := exec.CommandContext(ctx, "git", c.at("show", "HEAD:"+filepath.ToSlash(rel))...).Output()
	if err != nil {
		return nil, fmt.Errorf("git show HEAD:%s: %w", rel, err)
	}
	return out, nil
}

func (c CLI) toplevel(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("rev-parse", "--show-toplevel")...).Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Move renames a path in the working tree and the git index as one operation
// via `git mv`, so the rename is recorded atomically with the working-tree
// change. Production handlers.Mover.
func (c CLI) Move(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}
	if out, err := exec.Command("git", c.at("mv", src, dst)...).CombinedOutput(); err != nil {
		return fmt.Errorf("git mv %s %s: %s (%w)", src, dst, out, err)
	}
	return nil
}

// Checkout switches to branch, creating it first when create is set.
// Production handlers.Brancher.
func (c CLI) Checkout(branch string, create bool) error {
	args := []string{"checkout"}
	if create {
		args = append(args, "-b")
	}
	args = append(args, branch)
	if out, err := exec.Command("git", c.at(args...)...).CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout: %s (%w)", out, err)
	}
	return nil
}

// BranchMerged reports whether branch is merged into the current HEAD.
func (c CLI) BranchMerged(branch string) bool {
	out, err := exec.Command("git", c.at("branch", "--merged")...).Output()
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		// git branch prefixes: * = current, + = worktree checkout
		name := strings.TrimLeft(line, " *+")
		name = strings.TrimSpace(name)
		if name == branch {
			return true
		}
	}
	return false
}

// DeleteBranch removes branch (-d, or -D when force is set).
func (c CLI) DeleteBranch(branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	if out, err := exec.Command("git", c.at("branch", flag, branch)...).CombinedOutput(); err != nil {
		return fmt.Errorf("git branch %s: %s (%w)", flag, out, err)
	}
	return nil
}

// IsClean reports whether the working tree has no uncommitted changes.
// Production handlers.Puller.
func (c CLI) IsClean(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("status", "--porcelain")...).Output()
	if err != nil {
		return false, fmt.Errorf("git status --porcelain: %w", err)
	}
	return strings.TrimSpace(string(out)) == "", nil
}

// MergePull runs a merge-only pull. --no-rebase forces a merge pull
// regardless of the user's pull.rebase config, so background sync never
// rewrites the shared graph's history.
func (c CLI) MergePull(ctx context.Context) (string, error) {
	if err := c.ensureMergeDriverOnce(ctx); err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "git", c.at("pull", "--no-rebase")...).CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg == "" {
			return "", fmt.Errorf("git pull --no-rebase: %w", err)
		}
		return "", fmt.Errorf("git pull --no-rebase: %s", msg)
	}
	return msg, nil
}

// RepoRoot returns the git repository root, falling back to cwd when not in
// a repository.
func RepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return os.Getwd()
	}
	return strings.TrimSpace(string(out)), nil
}

// RepoRootFor returns the repository root containing path, which need not be
// the process working directory. An empty string means path is not inside a
// repository, which a caller reads as "nothing to commit here".
//
// A path that does not exist yet resolves through its nearest existing
// ancestor, so a graph directory about to be created still names the
// repository it will live in.
func RepoRootFor(path string) string {
	dir := existingAncestor(path)
	if dir == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ErrGraphRepoMissing reports a graph directory that does not exist or lies
// in no git repository.
var ErrGraphRepoMissing = errors.New("graph directory is not in a git repository")

// GraphRepoRoot returns the root of the repository holding the existing graph
// directory graphDir. Unlike RepoRootFor it never falls back to an ancestor,
// so sync cannot act on a repository that does not hold the graph.
func GraphRepoRoot(graphDir string) (string, error) {
	info, err := os.Stat(graphDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s: %w", graphDir, ErrGraphRepoMissing)
	}
	out, err := exec.Command("git", "-C", graphDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", graphDir, ErrGraphRepoMissing)
	}
	return strings.TrimSpace(string(out)), nil
}

// Repositories reaches repositories by path rather than through one adapter's
// Dir: a work repository's init sets its project up in a repository beside it.
// It is the production handlers.ProjectRepositories.
type Repositories struct{}

// RootOf is RepoRootFor that fails loud: only git's own "not a git
// repository" answer means no repository, so a setup never runs `git init`
// inside a repository git could not read.
func (Repositories) RootOf(dir string) (string, error) {
	ancestor := existingAncestor(dir)
	if ancestor == "" {
		return "", nil
	}
	probe := exec.Command("git", "-C", ancestor, "rev-parse", "--show-toplevel")
	probe.Env = append(os.Environ(), "LC_ALL=C")
	var stderr strings.Builder
	probe.Stderr = &stderr
	out, err := probe.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if strings.Contains(stderr.String(), "not a git repository") {
		return "", nil
	}
	return "", fmt.Errorf("finding the repository holding %s: %s (%w)", dir, strings.TrimSpace(stderr.String()), err)
}

func (Repositories) Init(dir string) error {
	out, err := exec.Command("git", "init", "--quiet", dir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git init %s: %s (%w)", dir, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (Repositories) CurrentBranch(dir string) (string, error) { return CurrentBranch(dir) }

func (Repositories) RemoteURL(dir string) string { return RemoteURL(dir) }

func (Repositories) StableRoot(dir string) (string, error) { return StableRepoRoot(dir) }

// existingAncestor returns path, or the closest ancestor that exists on disk.
func existingAncestor(path string) string {
	dir, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// UserName reads git config user.name, returning an empty string when git is
// unavailable or the setting isn't configured. Best-effort — used only as a
// pre-filled default for the sdd init participant prompt.
func UserName() string {
	out, err := exec.Command("git", "config", "--get", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RemoteURL returns the repo's origin remote URL, or "" when no remote is
// configured — the local-only case for repo_id derivation.
func RemoteURL(repoRoot string) string {
	out, err := exec.Command("git", "-C", repoRoot, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CurrentBranch resolves the symbolic branch at repoRoot. Detached HEAD is
// returned as an error because it cannot seed durable mutation authority.
func CurrentBranch(repoRoot string) (string, error) {
	out, err := exec.Command("git", "-C", repoRoot, "symbolic-ref", "--quiet", "--short", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git current branch: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "", fmt.Errorf("git current branch is empty")
	}
	return branch, nil
}

// errExitCode returns the exit code when err is an exec.ExitError, or -1.
func errExitCode(err error) int {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	return -1
}
