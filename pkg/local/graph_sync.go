package local

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	app "github.com/networkteam/sdd/pkg/application"
)

// checkGraphSync runs the cooldown-gated sync check in the repository holding
// the graph of the project at sddDir: it fetches once the cooldown has passed
// and compares the branch with its upstream.
func checkGraphSync(ctx context.Context, sddDir string, cfg *model.PerRepoConfig) (model.SyncStatus, error) {
	f := finders.New(finders.Options{Config: cfg, GitSyncer: graphRepository(sddDir, cfg)})
	return f.SyncStatus(ctx, query.SyncStatusQuery{SDDDir: sddDir, RespectCooldown: true})
}

// pullGraph merges and publishes upstream graph commits under automatic sync
// (20261027-194536-d-tac-190), reading the remote-tracking ref the last check
// fetched.
func pullGraph(ctx context.Context, sddDir string, cfg *model.PerRepoConfig) error {
	repo := graphRepository(sddDir, cfg)
	handler := handlers.New(handlers.Options{
		GraphDir: meta.ResolveGraphDir(filepath.Dir(sddDir), cfg), GraphPuller: repo, Pusher: repo, SyncMode: model.ResolveSyncMode(cfg),
	})
	return handler.AutoPullGraph(ctx)
}

func graphRepository(sddDir string, cfg *model.PerRepoConfig) git.CLI {
	return git.CLI{Dir: git.RepoRootFor(meta.ResolveGraphDir(filepath.Dir(sddDir), cfg))}
}

// GraphPublisher pushes what a mutation committed under automatic sync. It
// runs after GitFinalizer and is independent of it, so a retried mutation
// whose commit already landed still publishes.
type GraphPublisher struct {
	Checkout string
	GraphDir string
}

func (GraphPublisher) Name() string { return "git-publish" }

func (p GraphPublisher) Finalize(ctx context.Context, _ app.AppliedMutation) error {
	lock, err := lockGraph(p.GraphDir)
	if err != nil {
		return err
	}
	defer unlock(lock)
	repo := git.CLI{Dir: p.Checkout}
	handler := handlers.New(handlers.Options{GraphDir: p.GraphDir, Pusher: repo, GraphPuller: repo, SyncMode: model.SyncModeAuto})
	return handler.PublishGraph(ctx)
}

// SyncGraph is the application's GraphSync over registered checkouts: the
// sync check and automatic pull in the checkout of branch, when that
// checkout's config sets sync.mode auto. A conflicting merge returns its
// notice instead of an error.
func (a *GitWorktreeAcquirer) SyncGraph(ctx context.Context, branch string) (string, error) {
	if a.resolveConfig == nil {
		return "", nil
	}
	checkout, err := a.resolveCheckout(ctx, app.MutationTarget{Project: a.project, Branch: branch})
	if err != nil {
		return "", err
	}
	cfg, err := a.resolveConfig(checkout)
	if err != nil {
		return "", err
	}
	if model.ResolveSyncMode(cfg) != model.SyncModeAuto {
		return "", nil
	}
	lock, err := lockGraph(meta.ResolveGraphDir(checkout, cfg))
	if err != nil {
		return "", err
	}
	defer unlock(lock)
	sddDir := filepath.Join(checkout, model.SDDDirName)
	status, err := checkGraphSync(ctx, sddDir, cfg)
	if err != nil {
		return "", fmt.Errorf("checking graph sync on %s: %w", branch, err)
	}
	if !status.State.Fetched() {
		return "", nil
	}
	err = pullGraph(ctx, sddDir, cfg)
	var conflict *handlers.MergeConflictError
	if errors.As(err, &conflict) {
		return conflict.Notice(), nil
	}
	return "", err
}
