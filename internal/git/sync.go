package git

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// The finders.GitSyncer surface: methods are deliberately non-chatty — each
// returns the minimal structured answer the sync finder needs, letting
// orchestration logic live in one place.

// InRepo reports whether the process is inside a git working tree.
func (c CLI) InRepo(ctx context.Context) bool {
	out, err := exec.CommandContext(ctx, "git", c.at("rev-parse", "--is-inside-work-tree")...).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// HasRemote reports whether the repository has at least one remote configured.
func (c CLI) HasRemote(ctx context.Context) bool {
	out, err := exec.CommandContext(ctx, "git", c.at("remote")...).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// UpstreamRef returns the upstream ref name for the current branch.
func (c CLI) UpstreamRef(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("rev-parse", "--abbrev-ref", "@{u}")...).Output()
	if err != nil {
		// Non-zero exit (typically 128) means no upstream is configured;
		// report as empty string rather than propagating so the finder can
		// emit a dedicated state instead of a generic error.
		if errExitCode(err) >= 0 {
			return "", nil
		}
		return "", fmt.Errorf("git rev-parse --abbrev-ref @{u}: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Fetch runs `git fetch` with no args.
func (c CLI) Fetch(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "git", c.at("fetch")...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("git fetch: %w", err)
		}
		return fmt.Errorf("git fetch: %s", msg)
	}
	return nil
}

// CountCommits counts commits in rangeSpec whose subject matches
// subjectPattern; an empty pattern counts every commit.
func (c CLI) CountCommits(ctx context.Context, rangeSpec, subjectPattern string) (int, error) {
	if subjectPattern == "" {
		out, err := exec.CommandContext(ctx, "git", c.at("rev-list", "--count", rangeSpec)...).Output()
		if err != nil {
			return 0, fmt.Errorf("git rev-list --count %s: %w", rangeSpec, err)
		}
		return strconv.Atoi(strings.TrimSpace(string(out)))
	}
	pattern, err := regexp.Compile(subjectPattern)
	if err != nil {
		return 0, fmt.Errorf("compiling subject pattern: %w", err)
	}
	out, err := exec.CommandContext(ctx, "git", c.at("log", "--format=%s", rangeSpec)...).Output()
	if err != nil {
		return 0, fmt.Errorf("git log %s: %w", rangeSpec, err)
	}
	return model.CountMatchingSubjects(string(out), pattern), nil
}

// ChangedPaths lists the repository-relative paths tip changes against its
// merge base with base: the net difference a merge of tip brings in,
// including changes a merge commit makes on its own. Renames count as a
// deletion and an addition, so both paths are listed.
func (c CLI) ChangedPaths(ctx context.Context, base, tip string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("-c", "core.quotePath=false", "diff", "--name-only", "--no-renames", base+"..."+tip)...).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s...%s: %w", base, tip, err)
	}
	var paths []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

// RepoPath returns path relative to the root of this adapter's repository.
func (c CLI) RepoPath(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("rev-parse", "--show-toplevel")...).Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	return filepath.Rel(root, abs)
}

// MergeTreePredict simulates a three-way merge in memory and returns the
// paths that would conflict.
func (c CLI) MergeTreePredict(ctx context.Context, ourRef, theirRef string) ([]string, error) {
	// --no-messages suppresses the trailing informational/conflict-message
	// section so stdout after the OID is purely the conflicted path list.
	// --merge-base is omitted so git computes the base internally (2.38
	// compatibility — the explicit flag was added in 2.40).
	cmd := exec.CommandContext(ctx, "git", c.at("-c", "core.quotePath=false", "merge-tree", "--write-tree", "--name-only", "--no-messages", ourRef, theirRef)...)
	out, err := cmd.Output()
	if err != nil {
		// Exit 1 is the documented conflict signal; parse stdout for paths.
		if errExitCode(err) == 1 {
			return model.ParseMergeTreeConflicts(string(out)), nil
		}
		return nil, fmt.Errorf("git merge-tree: %w", err)
	}
	return model.ParseMergeTreeConflicts(string(out)), nil
}

// CommonDir returns the repository's common git directory, the one every
// worktree of it shares.
func (c CLI) CommonDir(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("rev-parse", "--path-format=absolute", "--git-common-dir")...).Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// RevParse returns the commit ref names.
func (c CLI) RevParse(ctx context.Context, ref string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("rev-parse", "--verify", "--quiet", ref+"^{commit}")...).Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Contains reports whether commit is reachable from HEAD. A commit the
// repository does not know is not contained.
func (c CLI) Contains(ctx context.Context, commit string) (bool, error) {
	err := exec.CommandContext(ctx, "git", c.at("merge-base", "--is-ancestor", commit, "HEAD")...).Run()
	if err == nil {
		return true, nil
	}
	if code := errExitCode(err); code == 1 || code == 128 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s HEAD: %w", commit, err)
}
