package finders

import (
	"context"

	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// Hooks lists every declared hook with whether it may run on this machine.
func (f *Finder) Hooks(ctx context.Context, q query.HooksQuery) (*query.HooksResult, error) {
	resolved, err := hooks.New(q.Sources, hooks.NewAllows(q.StateRoot)).Hooks(ctx)
	if err != nil {
		return nil, err
	}
	return &query.HooksResult{Hooks: resolved}, nil
}

// HookReviews returns what a person sees before allowing each repository
// whose hooks this machine has not allowed as they are now.
func (f *Finder) HookReviews(ctx context.Context, q query.HookReviewsQuery) ([]model.HookReview, error) {
	return hooks.New(q.Sources, hooks.NewAllows(q.StateRoot)).Reviews(ctx)
}
