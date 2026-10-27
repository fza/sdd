package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// CheckContribution applies contribution mode to a write on the checkout's
// current branch: ref is the configured upstream ref, defaultBranch the
// repository's default branch.
func (c CLI) CheckContribution(ctx context.Context, ref, defaultBranch string) error {
	branch, err := c.currentBranch(ctx)
	if err != nil {
		return err
	}
	resolved, err := exec.CommandContext(ctx, "git", c.at("rev-parse", "--verify", "--quiet", ref+"^{commit}")...).Output()
	if err != nil || strings.TrimSpace(string(resolved)) == "" {
		return fmt.Errorf("%w: the contribution ref %s does not resolve; fetch it first", model.ErrContributionRefused, ref)
	}
	fullName, err := exec.CommandContext(ctx, "git", c.at("rev-parse", "--symbolic-full-name", ref)...).Output()
	if err != nil {
		return fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	containsRef := false
	if branch != "" {
		containsRef, err = c.isAncestor(ctx, ref, "HEAD")
		if err != nil {
			return err
		}
	}
	return model.ContributionRefusal(model.ContributionTarget{
		Ref: ref, RefBranch: refBranch(strings.TrimSpace(string(fullName))),
		Branch: branch, DefaultBranch: defaultBranch, ContainsRef: containsRef,
	})
}

func (c CLI) currentBranch(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", c.at("symbolic-ref", "--quiet", "--short", "HEAD")...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("git symbolic-ref: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (c CLI) isAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	err := exec.CommandContext(ctx, "git", c.at("merge-base", "--is-ancestor", ancestor, descendant)...).Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("git merge-base --is-ancestor %s %s: %w", ancestor, descendant, err)
	}
	return true, nil
}

// refBranch names the branch a full ref points at: main for
// refs/remotes/origin/main and refs/heads/main, empty for anything else.
func refBranch(fullName string) string {
	if rest, ok := strings.CutPrefix(fullName, "refs/heads/"); ok {
		return rest
	}
	if rest, ok := strings.CutPrefix(fullName, "refs/remotes/"); ok {
		_, branch, found := strings.Cut(rest, "/")
		if found {
			return branch
		}
	}
	return ""
}
