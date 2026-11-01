package cliapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/networkteam/slogutils"
	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/repos"
)

const (
	cacheCommandName = "cache"
	scheduledFlag    = "scheduled"
)

func cacheCmd() *cli.Command {
	return &cli.Command{
		Name:  cacheCommandName,
		Usage: "Manage the machine-global cache",
		Commands: []*cli.Command{{
			Name:  "prune",
			Usage: "Delete unused cache data now: replaced and unused index versions, unused index stores, graph snapshots of vanished folders, clones of disconnected repos",
			Description: `sdd runs this in the background at most once a day. Data unused for
cache.retention (user-global config, default ` + model.DefaultCacheRetention + `) goes; versions the
current checkout searches always stay. sdd index gc removes more on request.`,
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:   scheduledFlag,
					Usage:  "Run as the daily background pass: skip when one started within a day, print nothing",
					Hidden: true,
				},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				loc, err := repos.DefaultLocations()
				if err != nil {
					return err
				}
				var keep handlers.CurrentHashSource
				if graphDir, err := resolveGraphDir(cmd); err == nil {
					reader, err := newReadFinder()
					if err != nil {
						return err
					}
					keep = handlers.NewIndexHandler(handlers.IndexHandlerOptions{GraphDir: graphDir, Reader: reader, Stderr: cmd.ErrWriter})
				} else if !errors.Is(err, errNoProject) {
					return err
				}
				scheduled := cmd.Bool(scheduledFlag)
				h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
					CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc), Keep: keep,
				})
				return h.Prune(ctx, &command.PruneCacheCmd{
					Scheduled: scheduled,
					OnPruned: func(c model.CachePruneCounts) {
						if scheduled {
							return
						}
						presenters.RenderResultLine(cmd.Writer, "pruned the cache", pruneDetail(c))
					},
				})
			},
		}},
	}
}

func pruneDetail(c model.CachePruneCounts) string {
	return fmt.Sprintf("%d index versions, %d index stores, %d graph snapshots, %d clones · %s freed",
		c.Versions, c.Stores, c.Snapshots, c.Clones, presenters.HumanBytes(c.Bytes))
}

// backgroundCachePrune launches the daily pruning pass when one is due and
// shows a failed pass once. Neither ever fails or delays the command: a
// problem here is a warning, since pruning is the machine's upkeep, not the
// command's work.
func backgroundCachePrune(ctx context.Context, launch func(dir string) error, notify bool) {
	log := slogutils.FromContext(ctx)
	loc, err := repos.DefaultLocations()
	if err != nil {
		log.Warn("background cache pruning: resolving the state dir", "err", err)
		return
	}
	if notify {
		notice, err := cacheprune.TakeFailureNotice(ctx, loc.StateRoot)
		switch {
		case err != nil:
			log.Warn("background cache pruning: reading its status", "err", err)
		case notice != "":
			log.Warn(notice)
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		log.Warn("background cache pruning: getting working directory", "err", err)
		return
	}
	if err := cacheprune.LaunchIfDue(ctx, loc.StateRoot, time.Now(), func() error { return launch(dir) }); err != nil {
		log.Warn("background cache pruning", "err", err)
	}
}

// cachePruneCheckInterval is how often a running sdd serve asks whether the
// daily pass is due; the due check itself keeps runs a day apart.
const cachePruneCheckInterval = time.Hour

// runCachePruneTimer keeps a long-running server launching the daily pass
// until ctx ends; the launch at server start is the root command's.
func runCachePruneTimer(ctx context.Context, launch func(dir string) error) {
	ticker := time.NewTicker(cachePruneCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			backgroundCachePrune(ctx, launch, false)
		}
	}
}

// serveCacheNotice is the MCP server's CacheNotice: a failed run's notice,
// handed out once.
func serveCacheNotice(ctx context.Context) string {
	loc, err := repos.DefaultLocations()
	if err != nil {
		slogutils.FromContext(ctx).Warn("background cache pruning: resolving the state dir", "err", err)
		return ""
	}
	notice, err := cacheprune.TakeFailureNotice(ctx, loc.StateRoot)
	if err != nil {
		slogutils.FromContext(ctx).Warn("background cache pruning: reading its status", "err", err)
		return ""
	}
	return notice
}

// renderCachePruneStatus prints the latest pruning run for sdd info and the
// gc report.
func renderCachePruneStatus(w io.Writer) error {
	loc, err := repos.DefaultLocations()
	if err != nil {
		return err
	}
	status, err := cacheprune.Load(loc.StateRoot)
	if err != nil {
		return err
	}
	presenters.RenderCachePruneStatus(w, status)
	return nil
}
