package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// The former repos.Git surface for the connected-repo caches, kept until the
// sdd-owned clones replace those caches.

// Clone clones url into dir.
func (CLI) Clone(ctx context.Context, url, dir string) error {
	return runRetired(ctx, "", "clone", "--quiet", url, dir)
}

// PullFFOnly pulls the checkout at dir, fast-forward only.
func (CLI) PullFFOnly(ctx context.Context, dir string) error {
	return runRetired(ctx, dir, "pull", "--quiet", "--ff-only")
}

func runRetired(ctx context.Context, dir string, args ...string) error {
	verb := args[0]
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("git %s: %w", verb, ctxErr)
		}
		msg := bytes.TrimSpace(stderr.Bytes())
		if len(msg) > 0 {
			return fmt.Errorf("git %s: %w: %s", verb, err, msg)
		}
		return fmt.Errorf("git %s: %w", verb, err)
	}
	return nil
}
