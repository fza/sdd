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

// pathEnv is env, nil meaning the process environment, without the inherited
// repositoryVariables, for a git command that locates its repository by path.
// A variable the caller set itself stays, such as a temporary index.
func pathEnv(env []string) []string {
	inherited := os.Environ()
	if env == nil {
		env = inherited
	}
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(repositoryVariables, name) && slices.Contains(inherited, kv)
	})
}

// CommandAt is git run with args in the repository at dir, whatever
// repository an inherited GIT_DIR names (pathEnv).
func CommandAt(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = pathEnv(nil)
	return cmd
}
