package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/networkteam/sdd/internal/model"
)

// MergeDriverCommand is the driver command git runs through sh for a
// file-level merge of a graph entry (20261030-085158-d-tac-vyt): ancestor,
// current and other version, conflict marker size, path. Any exit of `sdd
// merge-entry` but merged (0) or conflicted (1), such as an sdd without the
// command or none on PATH, falls back to git's text merge
// (20261030-093341-d-tac-t6j).
const MergeDriverCommand = "sdd merge-entry %O %A %B %L %P; s=$?; [ $s -le 1 ] && exit $s; " +
	"git merge-file -L ours -L base -L theirs --marker-size=%L %A %O %B"

// mergeDriverPrefix marks a driver value sdd wrote, which an upgrade replaces.
const mergeDriverPrefix = "sdd merge-entry "

// mergeDriverEnsured remembers the repositories whose driver section this
// process already checked, so a command committing many times asks git once.
var mergeDriverEnsured sync.Map

// EnsureMergeDriver writes the entry merge driver section into the
// repository's local config when it has none or holds a command an earlier sdd
// wrote. A driver someone configured otherwise is left as it is, and a
// directory outside any repository has no config to write.
func (c CLI) EnsureMergeDriver(ctx context.Context) error {
	_, err := c.ensureMergeDriver(ctx)
	return err
}

// ensureMergeDriver reports whether a repository was found.
func (c CLI) ensureMergeDriver(ctx context.Context) (bool, error) {
	found, err := c.inWorkTree(ctx)
	if err != nil || !found {
		return false, err
	}
	key := "merge." + model.EntryMergeDriver + ".driver"
	current, err := c.localConfig(ctx, key)
	if err != nil {
		return true, err
	}
	if current == MergeDriverCommand || (current != "" && !strings.HasPrefix(current, mergeDriverPrefix)) {
		return true, nil
	}
	settings := [][2]string{
		{"merge." + model.EntryMergeDriver + ".name", "sdd entry title and summary merge"},
		{key, MergeDriverCommand},
	}
	for _, setting := range settings {
		out, writeErr := exec.CommandContext(ctx, "git", c.at("config", "--local", setting[0], setting[1])...).CombinedOutput()
		if writeErr == nil {
			continue
		}
		// Another sdd process writing the same section holds git's config
		// lock; its write is the one this would make.
		if written, err := c.localConfig(ctx, key); err == nil && written == MergeDriverCommand {
			return true, nil
		}
		return true, fmt.Errorf("writing %s: %s (%w)", setting[0], strings.TrimSpace(string(out)), writeErr)
	}
	return true, nil
}

// localConfig returns the repository-local value of key, or "" when unset.
func (c CLI) localConfig(ctx context.Context, key string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("config", "--local", "--get", key)...).CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if errExitCode(err) == 1 {
		return "", nil
	}
	return "", fmt.Errorf("reading %s: %s (%w)", key, strings.TrimSpace(string(out)), err)
}

// inWorkTree reports whether the adapter's directory lies in a git work tree.
// Only git's own "not a git repository" answer counts as outside one; any
// other failure, such as an unsafe owner or a broken repository, is an error.
func (c CLI) inWorkTree(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("rev-parse", "--is-inside-work-tree")...).CombinedOutput()
	answer := strings.TrimSpace(string(out))
	if err == nil {
		return answer == "true", nil
	}
	if errExitCode(err) == 128 && strings.Contains(answer, "not a git repository") {
		return false, nil
	}
	return false, fmt.Errorf("locating the repository: %s (%w)", answer, err)
}

// ensureMergeDriverOnce runs EnsureMergeDriver for this repository the first
// time this process finds it. Every commit and merge path calls it, so a
// clone that never ran sdd init gains the driver from any sdd command writing
// to its graph.
func (c CLI) ensureMergeDriverOnce(ctx context.Context) error {
	dir := c.Dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolving the working directory: %w", err)
		}
		dir = wd
	}
	if _, done := mergeDriverEnsured.Load(dir); done {
		return nil
	}
	found, err := c.ensureMergeDriver(ctx)
	if err != nil {
		return fmt.Errorf("setting up the entry merge driver: %w", err)
	}
	if found {
		mergeDriverEnsured.Store(dir, struct{}{})
	}
	return nil
}

// MergeFile three-way merges other into current against base with `git
// merge-file`, writing the result, conflict markers included, into current.
// conflicted reports whether markers were left.
func (CLI) MergeFile(ctx context.Context, current, base, other string, markerSize int) (conflicted bool, err error) {
	args := []string{"merge-file", "-L", "ours", "-L", "base", "-L", "theirs"}
	if markerSize > 0 {
		args = append(args, "--marker-size="+strconv.Itoa(markerSize))
	}
	args = append(args, current, base, other)
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
	if err == nil {
		return false, nil
	}
	// git merge-file exits with the number of conflicts, capped at 127; a
	// negative status (255 as seen here) is a failure to merge at all.
	if code := errExitCode(err); code > 0 && code < 128 {
		return true, nil
	}
	return false, fmt.Errorf("git merge-file %s: %s (%w)", current, strings.TrimSpace(string(out)), err)
}
