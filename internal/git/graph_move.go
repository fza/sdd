package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// moveSplitRef holds a split commit while another repository fetches it.
const moveSplitRef = "refs/sdd-move/split"

// ErrNoSubtree reports a git without its subtree command, which carries a
// graph's history into or out of a separate repository.
var ErrNoSubtree = errors.New("git subtree is not installed: install the git subtree command (it ships in git's contrib), then run the move again")

// RequireSubtree fails with ErrNoSubtree when git has no subtree command.
func (c CLI) RequireSubtree(ctx context.Context) error {
	out, _ := exec.CommandContext(ctx, "git", "subtree", "-h").CombinedOutput()
	if strings.Contains(string(out), "git subtree") && !strings.Contains(string(out), "not a git command") {
		return nil
	}
	return ErrNoSubtree
}

// SplitSubtree is the history of prefix up to revision as commits holding
// prefix's content at their root, the original commits untouched.
func (c CLI) SplitSubtree(ctx context.Context, prefix, revision string) (string, error) {
	out, err := c.combined(ctx, nil, "", "subtree", "split", "--quiet", "--prefix="+filepath.ToSlash(prefix), revision)
	if err != nil {
		return "", err
	}
	return lastLine(out), nil
}

// FetchCommitFrom copies commit and its history from the repository at
// sourceDir into this one, without a ref left behind in either.
func (c CLI) FetchCommitFrom(ctx context.Context, sourceDir, commit string) error {
	source := CLI{Dir: sourceDir}
	_, err := source.combined(ctx, nil, "", "update-ref", moveSplitRef, commit)
	if err != nil {
		return err
	}
	_, fetchErr := c.combined(ctx, nil, "", "fetch", "--quiet", "--no-tags", sourceDir, moveSplitRef)
	_, deleteErr := source.combined(ctx, nil, "", "update-ref", "-d", moveSplitRef)
	return errors.Join(fetchErr, deleteErr)
}

// BuildCommit writes the commit plan describes without touching the working
// tree, the index or any branch, and returns it.
func (c CLI) BuildCommit(ctx context.Context, plan model.MoveCommit) (string, error) {
	gitDir, err := c.combined(ctx, nil, "", "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	index := filepath.Join(lastLine(gitDir), "sdd-move-index")
	defer func() { _ = os.Remove(index) }()
	env := append(os.Environ(), "GIT_INDEX_FILE="+index)
	if len(plan.Parents) > 0 && plan.Parents[0] != "" {
		_, err = c.combined(ctx, env, "", "read-tree", plan.Parents[0])
	} else {
		_, err = c.combined(ctx, env, "", "read-tree", "--empty")
	}
	if err != nil {
		return "", err
	}
	for _, path := range plan.Remove {
		_, err = c.combined(ctx, env, "", "rm", "--cached", "-r", "-q", "--ignore-unmatch", "--", filepath.ToSlash(path))
		if err != nil {
			return "", err
		}
	}
	if plan.Graft != "" {
		_, err = c.combined(ctx, env, "", "read-tree", "--prefix="+strings.TrimSuffix(filepath.ToSlash(plan.GraftAt), "/")+"/", plan.Graft)
		if err != nil {
			return "", err
		}
	}
	for _, file := range plan.Files {
		path := filepath.ToSlash(file.Path)
		if file.Remove {
			_, err = c.combined(ctx, env, "", "rm", "--cached", "-q", "--ignore-unmatch", "--", path)
			if err != nil {
				return "", err
			}
			continue
		}
		blob, err := c.combined(ctx, env, string(file.Content), "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		_, err = c.combined(ctx, env, "", "update-index", "--add", "--cacheinfo", "100644,"+lastLine(blob)+","+path)
		if err != nil {
			return "", err
		}
	}
	tree, err := c.combined(ctx, env, "", "write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", lastLine(tree)}
	for _, parent := range plan.Parents {
		if parent != "" {
			args = append(args, "-p", parent)
		}
	}
	commit, err := c.combined(ctx, env, plan.Message, append(args, "-F", "-")...)
	if err != nil {
		return "", err
	}
	return lastLine(commit), nil
}

// Land moves the checked-out branch forward to commit and updates the working
// tree to it, refusing anything but a fast-forward.
func (c CLI) Land(ctx context.Context, commit string) error {
	head, err := c.Head(ctx)
	if err != nil {
		return err
	}
	if head != "" {
		_, err = c.combined(ctx, nil, "", "merge", "--quiet", "--ff-only", commit)
		return err
	}
	_, err = c.combined(ctx, nil, "", "read-tree", "-m", "-u", commit)
	if err != nil {
		return err
	}
	_, err = c.combined(ctx, nil, "", "update-ref", "HEAD", commit)
	return err
}

// ChangedSince reports whether a commit between revision and ref touches
// prefix.
func (c CLI) ChangedSince(ctx context.Context, revision, ref, prefix string) (bool, error) {
	out, err := c.combined(ctx, nil, "", "rev-list", "--count", revision+".."+ref, "--", filepath.ToSlash(prefix))
	if err != nil {
		return false, err
	}
	return lastLine(out) != "0", nil
}

// BackfillHistory fetches every file content the history of HEAD lacks, in a
// partial clone; it needs a git gate admission.
func (c CLI) BackfillHistory(ctx context.Context) error {
	env, err := admittedEnv(ctx, "backfill", "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND="+sshCommand)
	if err != nil {
		return err
	}
	_, err = c.combined(ctx, env, "", "backfill")
	return err
}

// FileHistory is the patch series of the commits that touch paths, oldest
// first, with paths relative to prefix.
func (c CLI) FileHistory(ctx context.Context, prefix string, paths []string) ([]byte, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	args := append([]string{"log", "--reverse", "--format=%H", "--"}, slashed(paths)...)
	out, err := c.combined(ctx, nil, "", args...)
	if err != nil {
		return nil, err
	}
	var series bytes.Buffer
	for commit := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if commit == "" {
			continue
		}
		args := []string{"format-patch", "-k", "--stdout", "--binary", "--relative=" + filepath.ToSlash(prefix), "-1", commit, "--"}
		patch, err := c.combined(ctx, nil, "", append(args, slashed(paths)...)...)
		if err != nil {
			return nil, err
		}
		series.WriteString(patch)
	}
	return series.Bytes(), nil
}

// ApplyHistory commits a patch series from FileHistory under directory,
// keeping each commit's author and message.
func (c CLI) ApplyHistory(ctx context.Context, series []byte, directory string) error {
	if len(series) == 0 {
		return nil
	}
	_, err := c.combined(ctx, nil, string(series), "am", "-k", "--quiet", "--keep-cr", "--directory="+filepath.ToSlash(directory))
	if err != nil {
		_, abortErr := c.combined(ctx, nil, "", "am", "--abort")
		return errors.Join(err, abortErr)
	}
	return nil
}

// CommitAllowEmpty commits what paths hold now, or nothing beside the
// message when they are unchanged.
func (c CLI) CommitAllowEmpty(ctx context.Context, message string, paths ...string) error {
	// A path git mv already staged away matches nothing on disk or in the
	// index, and would fail the pathspec.
	var staged []string
	for _, path := range paths {
		_, statErr := os.Stat(filepath.Join(c.Dir, path))
		tracked, err := c.tracked(ctx, path)
		if err != nil {
			return err
		}
		if statErr == nil || tracked {
			staged = append(staged, path)
		}
	}
	if len(staged) > 0 {
		_, err := c.combined(ctx, nil, "", append([]string{"add", "--all", "--"}, slashed(staged)...)...)
		if err != nil {
			return err
		}
	}
	args := []string{"commit", "--quiet", "--allow-empty", "-m", message, "--"}
	for _, path := range paths {
		_, inHeadErr := c.combined(ctx, nil, "", "cat-file", "-e", "HEAD:"+filepath.ToSlash(path))
		if slices.Contains(staged, path) || inHeadErr == nil {
			args = append(args, filepath.ToSlash(path))
		}
	}
	_, err := c.combined(ctx, nil, "", args...)
	if err != nil {
		return err
	}
	return c.RecordGraphCloneCommit(ctx)
}

// MoveTracked moves a tracked or untracked file in the working tree and
// stages both sides.
func (c CLI) MoveTracked(ctx context.Context, from, to string) error {
	err := os.MkdirAll(filepath.Join(c.Dir, filepath.Dir(to)), 0o755)
	if err != nil {
		return err
	}
	tracked, err := c.tracked(ctx, from)
	if err != nil {
		return err
	}
	if tracked {
		_, err = c.combined(ctx, nil, "", "mv", "--", filepath.ToSlash(from), filepath.ToSlash(to))
		return err
	}
	err = os.Rename(filepath.Join(c.Dir, from), filepath.Join(c.Dir, to))
	if err != nil {
		return err
	}
	_, err = c.combined(ctx, nil, "", "add", "--", filepath.ToSlash(to))
	return err
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func slashed(paths []string) []string {
	out := make([]string, len(paths))
	for i, path := range paths {
		out[i] = filepath.ToSlash(path)
	}
	return out
}

// RecordCloneCommitsSince records, in an sdd-owned clone, every commit HEAD
// holds beyond since (all of HEAD's history when since is empty) as sdd's
// own, so the clone's upkeep keeps them; outside a clone it does nothing.
func (c CLI) RecordCloneCommitsSince(ctx context.Context, since string) error {
	if !c.graphClone() {
		return nil
	}
	span := "HEAD"
	if since != "" {
		span = since + "..HEAD"
	}
	out, err := c.combined(ctx, nil, "", "rev-list", "--reverse", span)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(c.Dir, ".git", model.GraphCloneCommits), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("recording the moved commits: %w", err)
	}
	_, err = file.WriteString(out)
	return errors.Join(err, file.Close())
}
