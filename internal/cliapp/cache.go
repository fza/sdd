package cliapp

import (
	"context"
	"errors"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/repos"
)

const cacheCommandName = "cache"

func cacheCmd() *cli.Command {
	return &cli.Command{
		Name:  cacheCommandName,
		Usage: "Manage the machine-global cache",
		Commands: []*cli.Command{{
			Name:  "prune",
			Usage: "Delete cache data past its retention now",
			Description: `Removes replaced index data after index.replaced_retention (default ` + model.DefaultReplacedRetention + `);
unused index stores, graph snapshots and embedder calibrations after
cache.retention (default ` + model.DefaultCacheRetention + `); graph snapshots of vanished folders and clones
sdd repo remove dropped that the repos list no longer names at once; and crash
leftovers a day old. Both keys live in the user-global config. Versions any
checkout read within cache.retention always stay. sdd index gc removes more
on request.`,
			Action: func(ctx context.Context, cmd *cli.Command) error {
				loc, err := repos.DefaultLocations()
				if err != nil {
					return err
				}
				var keep handlers.CurrentHashSource
				if graphDir, err := resolveGraphDir(cmd); err == nil {
					reader, err := newReadFinder(ctx)
					if err != nil {
						return err
					}
					keep = handlers.NewIndexHandler(handlers.IndexHandlerOptions{GraphDir: graphDir, Reader: reader, Stderr: cmd.ErrWriter})
				} else if !errors.Is(err, errNoProject) {
					return err
				}
				h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
					CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc), Keep: keep,
				})
				return h.Prune(ctx, &command.PruneCacheCmd{
					OnPruned: func(c model.CachePruneCounts) {
						presenters.RenderResultLine(cmd.Writer, "pruned the cache", presenters.CachePruneRemoved(c)+" · "+presenters.HumanBytes(c.Bytes)+" freed")
					},
				})
			},
		}},
	}
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
