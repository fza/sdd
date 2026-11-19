package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// CheckContribution applies contribution mode to a write on the current
// branch of the checkout at dir: ref is the configured upstream ref,
// defaultBranch the repository's default branch.
func CheckContribution(ctx context.Context, dir, ref, defaultBranch string) error {
	branch, err := currentBranch(ctx, dir)
	if err != nil {
		return err
	}
	resolved, err := gitIn(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Output()
	if err != nil || strings.TrimSpace(string(resolved)) == "" {
		return fmt.Errorf("%w: the contribution ref %s does not resolve; fetch it first", model.ErrContributionRefused, ref)
	}
	fullName, err := gitIn(ctx, dir, "rev-parse", "--symbolic-full-name", ref).Output()
	if err != nil {
		return fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	containsRef := false
	if branch != "" {
		containsRef, err = isAncestor(ctx, dir, ref, "HEAD")
		if err != nil {
			return err
		}
	}
	return model.ContributionRefusal(model.ContributionTarget{
		Ref: ref, RefBranch: refBranch(strings.TrimSpace(string(fullName))),
		Branch: branch, DefaultBranch: defaultBranch, ContainsRef: containsRef,
	})
}

func gitIn(ctx context.Context, dir string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
}

func currentBranch(ctx context.Context, dir string) (string, error) {
	out, err := gitIn(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("git symbolic-ref: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func isAncestor(ctx context.Context, dir, ancestor, descendant string) (bool, error) {
	err := gitIn(ctx, dir, "merge-base", "--is-ancestor", ancestor, descendant).Run()
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
