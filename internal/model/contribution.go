package model

import (
	"errors"
	"fmt"
)

// ErrContributionRefused marks a graph write contribution mode refuses: in a
// checkout whose machine-local config names an upstream ref, entries are
// written only on a feature branch cut from that ref (20261027-092131-d-tac-t7g).
var ErrContributionRefused = errors.New("contribution mode refuses the write")

// ContributionTarget is what contribution mode judges a write by. RefBranch is
// the branch the ref names (main for origin/main); ContainsRef reports whether
// Branch has the ref in its history.
type ContributionTarget struct {
	Ref           string
	RefBranch     string
	Branch        string
	DefaultBranch string
	ContainsRef   bool
}

// ContributionRefusal returns why contribution mode refuses a write on
// target, nil when it allows it.
func ContributionRefusal(target ContributionTarget) error {
	switch {
	case target.Branch == "":
		return fmt.Errorf("%w: the checkout has no branch checked out; capture on a feature branch cut from %s", ErrContributionRefused, target.Ref)
	case target.Branch == target.DefaultBranch:
		return fmt.Errorf("%w: %s is the default branch; capture on a feature branch cut from %s", ErrContributionRefused, target.Branch, target.Ref)
	case target.Branch == target.RefBranch:
		return fmt.Errorf("%w: %s is the branch of the contribution ref %s; capture on a feature branch cut from it", ErrContributionRefused, target.Branch, target.Ref)
	case !target.ContainsRef:
		return fmt.Errorf("%w: %s is not cut from %s", ErrContributionRefused, target.Branch, target.Ref)
	}
	return nil
}
