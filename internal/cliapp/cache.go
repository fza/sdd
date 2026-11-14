package cliapp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/networkteam/slogutils"
	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/syncstate"
	localadapter "github.com/networkteam/sdd/pkg/local"
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
			Usage: "Delete data nothing needs from sdd's cache, state and data folders now",
			Description: `sdd runs this in the background at most once a day. It removes ended
sessions after sessions.retention (default ` + model.DefaultSessionRetention + `); replaced index data after
index.replaced_retention (default ` + model.DefaultReplacedRetention + `); unused index stores, graph
snapshots, git gate records, calibrations and the state of unused projects
after cache.retention (default ` + model.DefaultCacheRetention + `); clones the repos list no longer names;
and crash leftovers a day old. The background pass also fetches the rule file
of gitleaks' newest release for credential scans while secrets.rules_update is
daily (the default). All four keys live in the user-global config. Open
sessions, unreadable session logs and the versions the current checkout
searches always stay. sdd index gc removes more on request.`,
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
				gate, err := commandGate()
				if err != nil {
					return err
				}
				scheduled := cmd.Bool(scheduledFlag)
				h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
					CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc),
					OwnedClones: syncstate.OwnedClones{Gate: gate}, Gate: gate, Keep: keep,
					State:         stateSweeper{localadapter.StateSweep{StateRoot: loc.StateRoot}},
					DeclaredHooks: hooks.DeclaredIn(loc.ConfigPath, repos.NewGraphClones(loc, git.GraphClones{}, gate).Dir),
					SecretRules:   secrets.NewUpdater(secretRulesDir(loc), secrets.NewGitHub("", "", nil), nil),
				})
				return h.Prune(ctx, &command.PruneCacheCmd{
					Scheduled: scheduled,
					OnPruned: func(c model.CachePruneCounts) {
						if scheduled {
							return
						}
						presenters.RenderResultLine(cmd.Writer, "pruned the cache", presenters.CachePruneRemoved(c)+" · "+presenters.HumanBytes(c.Bytes)+" freed")
					},
				})
			},
		}},
	}
}

// backgroundCachePrune launches the daily pruning pass when one is due and
// shows a failed pass once. Neither ever fails or delays the command: a
// problem here is a warning, since pruning is the machine's upkeep, not the
// command's work.
func backgroundCachePrune(ctx context.Context, launch func(dir, logPath string) error, notify bool) {
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
	if err := cacheprune.LaunchIfDue(ctx, loc.StateRoot, time.Now(), func() error { return launch(dir, filepath.Join(loc.StateRoot, cacheprune.LogFile)) }); err != nil {
		log.Warn("background cache pruning", "err", err)
	}
}

// cachePruneCheckInterval is how often a running sdd serve asks whether the
// daily pass is due; the due check itself keeps runs a day apart.
const cachePruneCheckInterval = time.Hour

// runCachePruneTimer keeps a long-running server launching the daily pass
// until ctx ends; the launch at server start is the root command's.
func runCachePruneTimer(ctx context.Context, launch func(dir, logPath string) error) {
	ticker := time.NewTicker(cachePruneCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			backgroundCachePrune(ctx, launch, false)
			useSecretRules(ctx)
		}
	}
}

func secretRulesDir(loc repos.Locations) string {
	return filepath.Join(loc.CacheRoot, secrets.CacheDirName)
}

// useSecretRules points credential scans at the rule file
// secrets.rules_update allows: the copies the daily pass fetches while it is
// daily, the built-in file while it is off. Called again in a running server,
// it picks up a copy fetched since. A config it cannot read leaves the rules
// in use, as the command reading that config fails on its own.
func useSecretRules(ctx context.Context) {
	log := slogutils.FromContext(ctx)
	loc, err := repos.DefaultLocations()
	if err != nil {
		log.Warn("secret rules: resolving the cache dir", "err", err)
		return
	}
	cfg, err := repos.LoadConfigFrom(loc.ConfigPath)
	if err != nil {
		log.Warn("secret rules: reading the global config", "err", err)
		return
	}
	dir := ""
	if model.ResolveSecretRulesUpdate(cfg.Secrets) == model.SecretRulesDaily {
		dir = secretRulesDir(loc)
	}
	secrets.Use(ctx, dir)
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

// stateSweeper is the local state sweep as the cache handler counts it.
type stateSweeper struct{ sweep localadapter.StateSweep }

func (s stateSweeper) Sweep(ctx context.Context, now time.Time, sessionRetention, unusedRetention time.Duration) (model.CachePruneCounts, error) {
	swept, err := s.sweep.Sweep(ctx, now, sessionRetention, unusedRetention)
	return model.CachePruneCounts{Sessions: swept.Sessions, Projects: swept.Projects, Unreadable: swept.Unreadable, Bytes: swept.Bytes}, err
}
