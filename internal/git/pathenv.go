package git

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// repositoryVariables are the variables git exports to hooks that pin a
// command to the hook's repository whatever -C names.
var repositoryVariables = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"}

// pathEnv is the process environment without repositoryVariables, for a git
// command that locates its repository by path.
func pathEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(repositoryVariables, name)
	})
}

// CommandAt is git run with args in the repository at dir, whatever
// repository an inherited GIT_DIR names (pathEnv).
func CommandAt(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = pathEnv()
	return cmd
}
