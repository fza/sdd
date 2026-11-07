package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/networkteam/sdd/internal/gitgate"
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
