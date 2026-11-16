package finders

import (
	"context"
	"errors"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// DependencyClosure lists the repos the local repo reaches through declared
// dependencies, walking DeclaredDependencies. Refreshing the clones is the
// handler's side effect (Handler.RefreshDependencyClosure), run before.
func (f *Finder) DependencyClosure(ctx context.Context, _ query.DependencyClosureQuery) ([]model.RepoID, error) {
	if f.cfg == nil {
		return nil, nil
	}
	var closure []model.RepoID
	for repoID, err := range model.DependencyClosure(ctx, f.cfg.RepoID, model.DependencyResolverFunc[model.RepoID](f.DeclaredDependencies)) {
		if err != nil {
			return nil, err
		}
		closure = append(closure, repoID)
	}
	return closure, nil
}

// DeclaredDependencies is one step of the dependency walk, as the clones
// stand: the local repo answers from its per-repo config, a connected repo
// from the committed config in its sdd-owned clone. A repo that is not
// connected, not cloned or without a config declares nothing, so it stays in the closure
// with nothing reached behind it.
func (f *Finder) DeclaredDependencies(_ context.Context, repoID model.RepoID) ([]model.RepoID, error) {
	if f.cfg != nil && repoID == f.cfg.RepoID {
		return f.cfg.Dependencies, nil
	}
	if f.repos == nil {
		return nil, nil
	}
	clone, connected, err := f.repos.Clone(repoID)
	if err != nil || !connected {
		return nil, err
	}
	cfg, err := clone.Config()
	if errors.Is(err, meta.ErrNotAnSDDProject) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return cfg.Dependencies, nil
}
