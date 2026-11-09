package finders

import (
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// Hooks lists every declared hook with its confirmation state.
func (f *Finder) Hooks(q query.HooksQuery) (*query.HooksResult, error) {
	sources := hooks.Sources{
		GlobalConfig: q.GlobalConfig,
		Project:      meta.Project{Root: q.Root, WorkRoot: q.WorkRoot, GraphRepo: q.GraphRepo},
		LocalConfig:  q.LocalConfig,
	}
	declared, err := hooks.Declared(sources)
	if err != nil {
		return nil, err
	}
	resolved, err := hooks.NewStore(q.StateRoot).Resolve(declared)
	if err != nil {
		return nil, err
	}
	return &query.HooksResult{Hooks: resolved}, nil
}

// HookReview returns what a person sees before confirming a hook: the files
// its hash covers with their digests and the head of its command file.
func (f *Finder) HookReview(q query.HookReviewQuery) (model.HookReview, error) {
	return hooks.Review(q.Hook)
}
