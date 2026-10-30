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

// MergeDriverCommand is the driver command git runs for a file-level merge of
// a graph entry (20261030-085158-d-tac-vyt): ancestor, current and other
// version, conflict marker size, path.
const MergeDriverCommand = "sdd merge-entry %O %A %B %L %P"

// mergeDriverEnsured remembers the repositories whose driver section this
// process already checked, so a command committing many times asks git once.
var mergeDriverEnsured sync.Map

// EnsureMergeDriver writes the entry merge driver section into the
// repository's local config when it has none. A driver someone configured
// differently is left as it is, and a directory outside any repository has no
// config to write.
func (c CLI) EnsureMergeDriver(ctx context.Context) error {
	dir := c.Dir
	if dir == "" {
		dir = "."
	}
	if !c.InWorkTree(ctx, dir) {
		return nil
	}
	key := "merge." + model.EntryMergeDriver + ".driver"
	out, err := exec.CommandContext(ctx, "git", c.at("config", "--local", "--get", key)...).CombinedOutput()
	if err == nil {
		return nil
	}
	if errExitCode(err) != 1 {
		return fmt.Errorf("reading %s: %s (%w)", key, strings.TrimSpace(string(out)), err)
	}
	settings := [][2]string{
		{"merge." + model.EntryMergeDriver + ".name", "sdd entry title and summary merge"},
		{key, MergeDriverCommand},
	}
	for _, setting := range settings {
		out, err := exec.CommandContext(ctx, "git", c.at("config", "--local", setting[0], setting[1])...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("writing %s: %s (%w)", setting[0], strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

// ensureMergeDriverOnce runs EnsureMergeDriver for this repository the first
// time this process touches it. Every commit and merge path calls it, so a
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
	if err := c.EnsureMergeDriver(ctx); err != nil {
		return err
	}
	mergeDriverEnsured.Store(dir, struct{}{})
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
