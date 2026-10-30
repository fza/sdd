package cliapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/urfave/cli/v3"
)

// mergeEntryCommandName is the subcommand git runs as the entry merge
// driver; the root Before hook keeps it free of anything touching the
// repository, since git runs it in the middle of a merge.
const mergeEntryCommandName = "merge-entry"

// Exit 1 tells the driver command a conflict was written; any other failure
// exits 2 so it falls back to git's text merge (20261030-093341-d-tac-t6j).
const (
	mergeEntryConflictExit = 1
	mergeEntryFailureExit  = 2
)

func mergeEntryCmd() *cli.Command {
	return &cli.Command{
		Name:      mergeEntryCommandName,
		Usage:     "Merge a graph entry file as git's merge driver (set up by sdd init)",
		ArgsUsage: "<base> <ours> <theirs> [<marker-size> [<path>]]",
		Hidden:    true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			err := runMergeEntry(ctx, cmd)
			switch {
			case err == nil:
				return nil
			case errors.Is(err, handlers.ErrEntryConflict):
				return cli.Exit(err, mergeEntryConflictExit)
			default:
				return cli.Exit(err, mergeEntryFailureExit)
			}
		},
	}
}

func runMergeEntry(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) < 3 || len(args) > 5 {
		return fmt.Errorf("merge-entry takes base, ours and theirs, then optionally the marker size and path; got %d arguments", len(args))
	}
	mergeCmd := &command.MergeEntryCmd{BasePath: args[0], OursPath: args[1], TheirsPath: args[2]}
	if len(args) > 3 {
		size, err := strconv.Atoi(args[3])
		if err != nil {
			return fmt.Errorf("reading the marker size %q: %w", args[3], err)
		}
		mergeCmd.MarkerSize = size
	}
	h := handlers.New(handlers.Options{FileMerger: git.CLI{}, Stderr: cmd.ErrWriter})
	return h.MergeEntry(ctx, mergeCmd)
}
