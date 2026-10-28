package local

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/syncstate"
	app "github.com/networkteam/sdd/pkg/application"
)

// GraphPublisher pushes what a mutation committed under automatic sync. It
// runs after GitFinalizer and is independent of it, so a retried mutation
// whose commit already landed still publishes.
type GraphPublisher struct {
	GraphDir string
}

func (GraphPublisher) Name() string { return "git-publish" }

func (p GraphPublisher) Finalize(ctx context.Context, _ app.AppliedMutation) error {
	opts, err := syncstate.HandlerOptions(ctx, p.GraphDir, handlers.Options{SyncMode: model.SyncModeAuto})
	if err != nil {
		return err
	}
	return handlers.New(opts).PublishGraph(ctx)
}

// SyncGraph runs one sync round in the checkout of branch when that
// checkout's config sets sync.mode auto. A round started by a call waits out
// the cooldown; a timer round always fetches and stamps the marker.
func (a *GitWorktreeAcquirer) SyncGraph(ctx context.Context, branch string, trigger app.SyncTrigger) error {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || model.ResolveSyncMode(cfg) != model.SyncModeAuto {
		return err
	}
	opts, err := syncstate.HandlerOptions(ctx, meta.ResolveGraphDir(checkout, cfg), handlers.Options{
		GraphDir: meta.ResolveGraphDir(checkout, cfg), SyncMode: model.SyncModeAuto,
	})
	if err != nil {
		return err
	}
	var cooldown time.Duration
	if trigger == app.SyncOnCall {
		cooldown = model.ResolveSyncCooldown(cfg)
	}
	log := slogutils.FromContext(ctx)
	ctx = handlers.WithSyncReport(ctx, func(block model.SyncBlocked) { log.Info(block.Banner()) })
	err = handlers.New(opts).SyncRound(ctx, &command.SyncRoundCmd{SDDDir: filepath.Join(checkout, model.SDDDirName), Cooldown: cooldown})
	if err != nil {
		return fmt.Errorf("syncing the graph of %s: %w", branch, err)
	}
	return nil
}

// SyncNotice claims the blocked-sync notice of branch's graph repository for
// session.
func (a *GitWorktreeAcquirer) SyncNotice(ctx context.Context, branch, session string) (string, error) {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || model.ResolveSyncMode(cfg) != model.SyncModeAuto {
		return "", err
	}
	graphDir := meta.ResolveGraphDir(checkout, cfg)
	opts, err := syncstate.HandlerOptions(ctx, graphDir, handlers.Options{GraphDir: graphDir})
	if err != nil {
		return "", err
	}
	var notice string
	err = handlers.New(opts).ClaimSyncNotice(ctx, &command.ClaimSyncNoticeCmd{Session: session, OnNotice: func(text string) { notice = text }})
	return notice, err
}

// ReleaseSyncOwner gives up session's ownership of the blocked sync of
// branch's graph repository.
func (a *GitWorktreeAcquirer) ReleaseSyncOwner(ctx context.Context, branch, session string) (string, error) {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || model.ResolveSyncMode(cfg) != model.SyncModeAuto {
		return "", err
	}
	opts, err := syncstate.HandlerOptions(ctx, meta.ResolveGraphDir(checkout, cfg), handlers.Options{})
	if err != nil {
		return "", err
	}
	var notice string
	err = handlers.New(opts).ReleaseSyncOwner(ctx, &command.ReleaseSyncOwnerCmd{Session: session, OnNotice: func(text string) { notice = text }})
	return notice, err
}

// SyncInterval is the sync.interval of branch's checkout under automatic
// sync.
func (a *GitWorktreeAcquirer) SyncInterval(ctx context.Context, branch string) (time.Duration, bool, error) {
	_, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil {
		return 0, false, err
	}
	interval, on := model.ResolveSyncInterval(cfg)
	return interval, on, nil
}

func (a *GitWorktreeAcquirer) syncCheckout(ctx context.Context, branch string) (string, *model.PerRepoConfig, error) {
	if a.resolveConfig == nil {
		return "", nil, nil
	}
	checkout, err := a.resolveCheckout(ctx, app.MutationTarget{Project: a.project, Branch: branch})
	if err != nil {
		return "", nil, err
	}
	cfg, err := a.resolveConfig(checkout)
	if err != nil {
		return "", nil, err
	}
	return checkout, cfg, nil
}

// SyncBlocked reads what blocks sync of branch's graph repository.
func (a *GitWorktreeAcquirer) SyncBlocked(ctx context.Context, branch string) (*app.SyncBlocked, error) {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || model.ResolveSyncMode(cfg) != model.SyncModeAuto {
		return nil, err
	}
	block, err := syncstate.ReadBlocked(ctx, meta.ResolveGraphDir(checkout, cfg))
	if err != nil || block == nil {
		return nil, err
	}
	return &app.SyncBlocked{
		Kind: string(block.Kind), Since: block.Since, Upstream: block.Upstream, Detail: block.Detail,
		Source: block.Source, Destination: block.Destination, Paths: block.Paths, Owner: block.Owner, Steps: block.Steps(),
	}, nil
}
