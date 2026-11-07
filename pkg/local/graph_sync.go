package local

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/syncstate"
	app "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
)

// GraphPublisher pushes what a mutation committed under automatic sync. It
// runs after GitFinalizer and is independent of it, so a retried mutation
// whose commit already landed still publishes.
type GraphPublisher struct {
	GraphDir string
	gate     *gitgate.Gate
}

func (GraphPublisher) Name() string { return "git-publish" }

func (p GraphPublisher) Finalize(ctx context.Context, _ app.AppliedMutation) error {
	opts, err := syncstate.HandlerOptions(ctx, p.GraphDir, handlers.Options{SyncMode: model.SyncModeAuto, Gate: p.gate})
	if err != nil {
		return err
	}
	return handlers.New(opts).PublishGraph(ctx)
}

// MergeDriverSetup sets up the entry merge driver in a mutation target's
// checkout. It runs after GitFinalizer and GraphPublisher, so a config it
// cannot write never holds back the commit or its push.
type MergeDriverSetup struct {
	Checkout string
}

func (MergeDriverSetup) Name() string { return "git-merge-driver" }

func (s MergeDriverSetup) Finalize(ctx context.Context, _ app.AppliedMutation) error {
	err := git.CLI{Dir: s.Checkout}.EnsureMergeDriver(ctx)
	if err != nil {
		return fmt.Errorf("setting up the entry merge driver: %w", err)
	}
	return nil
}

// SyncGraph runs one sync round in the checkout of branch when it is an
// sdd-owned graph clone; its git operations run as far as the git gate admits
// them, whatever the trigger.
func (a *GitWorktreeAcquirer) SyncGraph(ctx context.Context, branch string, _ app.SyncTrigger) error {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || cfg == nil || syncstate.Mode(checkout) != model.SyncModeAuto {
		return err
	}
	opts, err := syncstate.HandlerOptions(ctx, meta.ResolveGraphDir(checkout, cfg), handlers.Options{
		GraphDir: meta.ResolveGraphDir(checkout, cfg), SyncMode: model.SyncModeAuto, Gate: a.gate,
	})
	if err != nil {
		return err
	}
	log := slogutils.FromContext(ctx)
	ctx = handlers.WithSyncReport(ctx, func(block model.SyncBlocked) { log.Info(block.Banner()) })
	err = handlers.New(opts).SyncRound(ctx, &command.SyncRoundCmd{})
	if err != nil {
		return fmt.Errorf("syncing the graph of %s: %w", branch, err)
	}
	return nil
}

// BackfillTitles titles the untitled entries in the checkout of branch,
// whatever its sync mode (20261105-233807-d-tac-831). A backfill started by a
// call waits out model.TitleBackfillCooldown; one from a session start or a
// timer always runs. Under automatic sync the next round pushes its commit.
func (a *GitWorktreeAcquirer) BackfillTitles(ctx context.Context, branch string, trigger app.SyncTrigger, runner llm.Runner) error {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || cfg == nil {
		return err
	}
	graphDir := meta.ResolveGraphDir(checkout, cfg)
	sddDir := filepath.Join(checkout, model.SDDDirName)
	opts, err := syncstate.TitlesOptions(ctx, graphDir, handlers.Options{
		SDDDir:    sddDir,
		Reader:    finders.New(finders.Options{Config: cfg}),
		LLMRunner: runner,
		Committer: git.CLI{Dir: git.RepoRootFor(graphDir)},
		Language:  cfg.Language,
	})
	if err != nil {
		return err
	}
	var cooldown time.Duration
	if trigger == app.SyncOnCall {
		cooldown = model.TitleBackfillCooldown
	}
	log := slogutils.FromContext(ctx)
	ctx = handlers.WithSyncReport(ctx, func(block model.SyncBlocked) { log.Info(block.Banner()) })
	err = handlers.New(opts).BackfillTitles(ctx, &command.BackfillTitlesCmd{SDDDir: sddDir, Cooldown: cooldown})
	if err != nil {
		return fmt.Errorf("titling the entries of %s: %w", branch, err)
	}
	return nil
}

// SyncNotice claims the blocked-sync notice of branch's graph repository for
// session. Under manual sync only a failed title backfill is served.
func (a *GitWorktreeAcquirer) SyncNotice(ctx context.Context, branch, session string) (string, error) {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || cfg == nil {
		return "", err
	}
	graphDir := meta.ResolveGraphDir(checkout, cfg)
	opts, err := syncstate.HandlerOptions(ctx, graphDir, handlers.Options{GraphDir: graphDir})
	if err != nil {
		return "", err
	}
	var notice string
	err = handlers.New(opts).ClaimSyncNotice(ctx, &command.ClaimSyncNoticeCmd{
		Session: session, TitlesOnly: syncstate.Mode(checkout) != model.SyncModeAuto, OnNotice: func(text string) { notice = text },
	})
	return notice, err
}

// ReleaseSyncOwner gives up session's ownership of the blocked sync of
// branch's graph repository.
func (a *GitWorktreeAcquirer) ReleaseSyncOwner(ctx context.Context, branch, session string) (string, error) {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || cfg == nil {
		return "", err
	}
	opts, err := syncstate.HandlerOptions(ctx, meta.ResolveGraphDir(checkout, cfg), handlers.Options{})
	if err != nil {
		return "", err
	}
	var notice string
	err = handlers.New(opts).ReleaseSyncOwner(ctx, &command.ReleaseSyncOwnerCmd{
		Session: session, TitlesOnly: syncstate.Mode(checkout) != model.SyncModeAuto, OnNotice: func(text string) { notice = text },
	})
	return notice, err
}

// SyncDue reports whether the sync round of branch's checkout has an
// operation the git gate would admit: its fetch, or its push with commits
// waiting. Only an sdd-owned graph clone syncs automatically.
func (a *GitWorktreeAcquirer) SyncDue(ctx context.Context, branch string) (bool, error) {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || cfg == nil || syncstate.Mode(checkout) != model.SyncModeAuto {
		return false, err
	}
	if a.gate == nil {
		return false, repos.ErrNoGate
	}
	root, err := git.GraphRepoRoot(meta.ResolveGraphDir(checkout, cfg))
	if err != nil {
		return false, err
	}
	repo := git.CLI{Dir: root}
	url, err := repo.UpstreamURL(ctx)
	if err != nil {
		return false, err
	}
	target, err := gitgate.TargetFromURL(url)
	if err != nil {
		return false, err
	}
	due, _, err := a.gate.Due(target, gitgate.Fetch)
	if err != nil || due {
		return due, err
	}
	upstream, err := repo.UpstreamRef(ctx)
	if err != nil || upstream == "" {
		return false, err
	}
	ahead, err := repo.CountCommits(ctx, "@{u}..HEAD", "")
	if err != nil || ahead == 0 {
		return false, err
	}
	due, _, err = a.gate.Due(target, gitgate.Push)
	return due, err
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

// SyncBlocked reads what blocks sync of branch's graph repository; under
// manual sync only a failed title backfill.
func (a *GitWorktreeAcquirer) SyncBlocked(ctx context.Context, branch string) (*app.SyncBlocked, error) {
	checkout, cfg, err := a.syncCheckout(ctx, branch)
	if err != nil || cfg == nil {
		return nil, err
	}
	block, err := syncstate.ReadBlocked(ctx, meta.ResolveGraphDir(checkout, cfg))
	if err != nil || block == nil {
		return nil, err
	}
	if syncstate.Mode(checkout) != model.SyncModeAuto && !block.TitlesFailed() {
		return nil, nil
	}
	return &app.SyncBlocked{
		Kind: string(block.Kind), Since: block.Since, Upstream: block.Upstream, Detail: block.Detail,
		Source: block.Source, Destination: block.Destination, Paths: block.Paths, Owner: block.Owner, Steps: block.Steps(),
	}, nil
}
