package local

import (
	"context"
	"os/exec"

	"github.com/networkteam/sdd/internal/git"
)

// runGitTargetCommand runs git for GitWorktreeAcquirer; a call naming its
// repository with -C reaches that repository through git.CommandAt.
func runGitTargetCommand(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) > 1 && args[0] == "-C" {
		return git.CommandAt(ctx, args[1], args[2:]...).CombinedOutput()
	}
	return exec.CommandContext(ctx, "git", args...).CombinedOutput()
}
