package model_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestContributionRefusal(t *testing.T) {
	base := model.ContributionTarget{Ref: "origin/main", RefBranch: "main", Branch: "feat/x", DefaultBranch: "main", ContainsRef: true}
	cases := []struct {
		name   string
		change func(*model.ContributionTarget)
		cause  string
	}{
		{name: "feature branch cut from the ref", change: func(*model.ContributionTarget) {}},
		{name: "detached checkout", change: func(c *model.ContributionTarget) { c.Branch = "" }, cause: "no branch checked out"},
		{name: "default branch", change: func(c *model.ContributionTarget) { c.Branch, c.DefaultBranch = "trunk", "trunk" }, cause: "trunk is the default branch"},
		{name: "the ref's branch", change: func(c *model.ContributionTarget) { c.Branch, c.DefaultBranch = "main", "trunk" }, cause: "main is the branch of the contribution ref origin/main"},
		{name: "branch not cut from the ref", change: func(c *model.ContributionTarget) { c.ContainsRef = false }, cause: "feat/x is not cut from origin/main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := base
			tc.change(&target)
			err := model.ContributionRefusal(target)
			if tc.cause == "" {
				if err != nil {
					t.Errorf("a feature branch cut from the ref should be allowed, got %v", err)
				}
				return
			}
			if !errors.Is(err, model.ErrContributionRefused) || !strings.Contains(err.Error(), tc.cause) {
				t.Errorf("the refusal should name its cause, got %v", err)
			}
		})
	}
}
