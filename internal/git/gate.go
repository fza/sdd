package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
)

// admittedEnv is the environment of a git command reaching a remote. A ctx
// the gate did not admit is refused, so no network operation runs outside
// the per-machine limits (20261107-095427-d-tac-yux).
func admittedEnv(ctx context.Context, verb string, extra ...string) ([]string, error) {
	gateEnv, admitted := gitgate.Env(ctx)
	if !admitted {
		return nil, fmt.Errorf("git %s: %w", verb, gitgate.ErrNotAdmitted)
	}
	env := append(os.Environ(), extra...)
	return append(env, gateEnv...), nil
}

// command is git run with args in the adapter's repository, with the
// environment localEnv gives it.
func (c CLI) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", c.at(args...)...)
	cmd.Env = c.localEnv(ctx)
	return cmd
}

// run is the package run in the adapter's repository, with the environment
// localEnv gives it.
func (c CLI) run(ctx context.Context, args ...string) error {
	return runEnv(ctx, c.localEnv(ctx), c.Dir, args...)
}

// localEnv is the environment of a git command in the repository: nil, so
// the process environment, outside an sdd-owned graph clone, and cloneEnv in
// one.
func (c CLI) localEnv(ctx context.Context) []string {
	if !c.graphClone() {
		return nil
	}
	return cloneEnv(ctx)
}

// cloneEnv lets a git command in an sdd-owned graph clone fetch file
// contents on its own only inside a gate admission, which counts and bounds
// the connection; outside one, a command that needs missing contents fails
// instead of connecting (20261107-224549-d-tac-r9q, 20261108-104247-d-tac-k6f).
func cloneEnv(ctx context.Context) []string {
	gateEnv, admitted := gitgate.Env(ctx)
	if admitted {
		env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND="+sshCommand)
		return append(env, gateEnv...)
	}
	if !lazyFetchBar() {
		return nil
	}
	return append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
}

// lazyFetchBar reports whether the installed git bars lazy fetches in a
// clone: only one whose backfill can supply what a merge needs first.
var lazyFetchBar = sync.OnceValue(func() bool {
	out, err := exec.Command("git", "version").Output()
	return err == nil && model.GitSupportsLazyFetchBar(string(out))
})

// ExpandURL applies the insteadOf rewrites git would apply to url, without
// reaching the remote: the gate limits the host git connects to.
func ExpandURL(ctx context.Context, url string) (string, error) {
	return gitOutput(ctx, "ls-remote", "--get-url", url)
}

// UpstreamURL is the URL of the remote the current branch fetches from,
// rewrites applied; origin when the branch names none. In a graph clone it
// is the protocol remote recorded last.
func (c CLI) UpstreamURL(ctx context.Context) (string, error) {
	var remote string
	var err error
	if c.graphClone() {
		remote, err = c.localConfig(ctx, remoteKey)
	} else {
		remote, err = c.branchRemote(ctx)
	}
	if err != nil {
		return "", err
	}
	if remote == "" {
		remote = "origin"
	}
	return c.output(ctx, "remote", "get-url", remote)
}

func (c CLI) branchRemote(ctx context.Context) (string, error) {
	branch, err := c.CurrentBranch(ctx)
	if err != nil || branch == "" {
		return "", err
	}
	out, err := gitOutput(ctx, c.at("config", "--get", "branch."+branch+".remote")...)
	if errExitCode(err) == 1 {
		return "", nil
	}
	return out, err
}

func gitOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
