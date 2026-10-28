package cliapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/syncstate"
	"github.com/networkteam/slogutils"
	"github.com/urfave/cli/v3"
)

// syncCheckTimeout caps the full sync check (fetch + log range + merge-tree).
// Bounded so broken networks don't freeze a command for minutes — git fetch
// alone can hang on unreachable remotes without this.
const syncCheckTimeout = 30 * time.Second

// syncCmd exposes graph synchronization: under automatic sync the bare
// command runs a round even while a conflict pauses sync, and --pull performs
// a merge-only pull so sync never rewrites the shared graph's history.
func syncCmd() *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "Synchronize the shared graph with the upstream branch",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "pull",
				Usage: "Pull upstream changes with a merge (never a rebase); refuses on a dirty working tree",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			dir, err := resolveGraphDir(cmd)
			if err != nil {
				return err
			}
			if cmd.Bool("pull") {
				handler := handlers.New(handlers.Options{Stderr: cmd.ErrWriter, Puller: graphGit(dir)})
				return handler.SyncPull(ctx, &command.SyncPullCmd{
					OnPulled: func(output string) {
						if output != "" {
							fmt.Fprintln(cmd.Writer, output)
						}
					},
				})
			}
			syncMode, err := graphSyncMode(dir)
			if err != nil {
				return err
			}
			if syncMode != model.SyncModeAuto {
				return errors.New("sync: specify --pull to merge upstream changes into the shared graph, or set sync.mode auto")
			}
			sddDir, err := resolveSDDDir()
			if err != nil {
				return err
			}
			opts, err := graphSyncOptions(ctx, dir, handlers.Options{SyncMode: syncMode})
			if err != nil {
				return err
			}
			return handlers.New(opts).SyncRound(ctx, &command.SyncRoundCmd{SDDDir: sddDir, Explicit: true})
		},
	}
}

// graphSyncOptions adds the git surfaces and sync state of graphDir's
// repository to opts under automatic sync.
func graphSyncOptions(ctx context.Context, graphDir string, opts handlers.Options) (handlers.Options, error) {
	if opts.SyncMode != model.SyncModeAuto {
		return opts, nil
	}
	return syncstate.HandlerOptions(ctx, graphDir, opts)
}

// withSyncBanner scopes blocked-sync reporting to one command: each blocked
// state prints once, as a plain line rather than a warning
// (20261028-204922-d-tac-gcl).
func withSyncBanner(ctx context.Context, w io.Writer) context.Context {
	return handlers.WithSyncReport(ctx, func(block model.SyncBlocked) {
		fmt.Fprintf(w, "%s; `sdd info` shows the record and how to clear it\n", block.Banner())
	})
}

// runSyncCheck runs the background sync check of every command: under
// automatic sync a round that skips when another process syncs, otherwise
// a fetch and a status line. Bounded by syncCheckTimeout so network
// pathologies cannot stall the CLI, and it never fails the command.
func runSyncCheck(ctx context.Context) {
	logger := slogutils.FromContext(ctx)

	sddDir, err := resolveSDDDir()
	if err != nil {
		// Outside an SDD repo (or no cwd) — nothing to sync. Silent: this
		// is the normal state for `sdd --help` and for the first `sdd init`.
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		logger.Debug("sync check: config unreadable", "err", err)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, syncCheckTimeout)
	defer cancel()

	graphDir := meta.ResolveGraphDir(filepath.Dir(sddDir), cfg)
	cooldown := model.ResolveSyncCooldown(cfg)
	if model.ResolveSyncMode(cfg) == model.SyncModeAuto {
		opts, err := graphSyncOptions(ctx, graphDir, handlers.Options{SyncMode: model.SyncModeAuto, GraphDir: graphDir})
		if err == nil {
			err = handlers.New(opts).SyncRound(ctx, &command.SyncRoundCmd{SDDDir: sddDir, Cooldown: cooldown})
		}
		if err != nil {
			logger.Warn(fmt.Sprintf("sync: %v", err))
		}
		return
	}

	repo, err := git.GraphRepoRoot(graphDir)
	if err != nil {
		logger.Debug("sync check: no graph repository", "err", err)
		return
	}
	graphRepo := git.CLI{Dir: repo}
	var state model.SyncState
	var reason string
	err = handlers.New(handlers.Options{GraphFetcher: graphRepo}).FetchGraph(ctx, &command.FetchGraphCmd{
		SDDDir: sddDir, Cooldown: cooldown,
		OnFetched: func(fetched model.SyncState, why string) { state, reason = fetched, why },
	})
	if err != nil {
		logger.Debug("sync check failed", "err", err)
		return
	}
	status := model.SyncStatus{State: state, Reason: reason}
	if state == model.SyncStateFetched {
		status, err = finders.New(finders.Options{Config: cfg, GitSyncer: graphRepo}).SyncStatus(ctx, query.SyncStatusQuery{})
		if err != nil {
			logger.Debug("sync check failed", "err", err)
			return
		}
	}
	emitSyncStatus(logger, status)
}

// emitSyncStatus renders a SyncStatus as one slog line. The phrasing is
// the skill's pattern-match surface — changes here must stay coordinated
// with `internal/bundledskills/templates/sdd/SKILL.md.tmpl`.
func emitSyncStatus(logger *slog.Logger, s model.SyncStatus) {
	switch s.State {
	case model.SyncStateUpToDate, model.SyncStateSkipped, model.SyncStateFetched:
		// Quiet: nothing to tell the user or the skill.
	case model.SyncStateFastForward:
		logger.Info(fmt.Sprintf("sync: fast-forward available, %d commits behind", s.RemoteAhead))
	case model.SyncStateCleanRebase:
		logger.Info(fmt.Sprintf("sync: merge is clean, %d remote / %d local", s.RemoteAhead, s.LocalAhead))
	case model.SyncStateConflictPredicted:
		logger.Warn(fmt.Sprintf("sync: merge would conflict in %s, %d remote / %d local",
			strings.Join(s.ConflictPaths, ", "), s.RemoteAhead, s.LocalAhead))
	case model.SyncStateLocalAhead:
		logger.Info(fmt.Sprintf("sync: local ahead by %d, consider push", s.LocalAhead))
	case model.SyncStateNoRepo:
		logger.Warn("sync: not a git repo")
	case model.SyncStateNoRemote:
		logger.Warn("sync: no remote configured")
	case model.SyncStateNoUpstream:
		logger.Warn("sync: no upstream for current branch — set with `git branch --set-upstream-to=origin/<branch>`")
	case model.SyncStateFetchFailed:
		logger.Warn(fmt.Sprintf("sync: fetch failed: %s", s.Reason))
	}
}

// syncBlocked reads what blocks automatic sync of the project's graph
// repository, nil under manual sync or while sync proceeds.
func syncBlocked(ctx context.Context) (*model.SyncBlocked, error) {
	sddDir, err := resolveSDDDir()
	if err != nil {
		return nil, nil
	}
	cfg, err := loadConfig()
	if err != nil || model.ResolveSyncMode(cfg) != model.SyncModeAuto {
		return nil, err
	}
	return syncstate.ReadBlocked(ctx, meta.ResolveGraphDir(filepath.Dir(sddDir), cfg))
}
