package git

import (
	"context"
	"fmt"
	"os/exec"
)

// CommitMessage returns the message of the commit hash names; found is false
// when the repository holds no such commit.
func (c CLI) CommitMessage(ctx context.Context, hash string) (string, bool, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", "--end-of-options", hash+"^{commit}")
	cmd.Dir = c.Dir
	err := cmd.Run()
	if errExitCode(err) == 1 {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("git rev-parse %s: %w", hash, err)
	}
	show := exec.CommandContext(ctx, "git", "log", "-1", "--format=%B", "--end-of-options", hash+"^{commit}")
	show.Dir = c.Dir
	out, err := show.Output()
	if err != nil {
		return "", false, fmt.Errorf("git log %s: %w", hash, err)
	}
	return string(out), true, nil
}
