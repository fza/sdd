package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// TrackedFiles is the production hooks.Tracker.
type TrackedFiles struct{}

// Tracked returns the files under paths, relative to root, that the
// repository at root tracks, the paths matched whatever their case, as a
// case-insensitive file system opens them. The repository is the one at
// root, whatever GIT_DIR the process inherited. A root in no repository git
// can read is an error: there is no saying what it tracks.
func (TrackedFiles) Tracked(ctx context.Context, root string, paths ...string) ([]string, error) {
	args := []string{"-C", root, "ls-files", "-z", "--"}
	for _, path := range paths {
		args = append(args, ":(icase)"+path)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = pathEnv(nil)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing the files git tracks under %s: %w: %s", root, err, strings.TrimSpace(stderr.String()))
	}
	var files []string
	for _, file := range strings.Split(string(out), "\x00") {
		if file != "" {
			files = append(files, file)
		}
	}
	return files, nil
}
