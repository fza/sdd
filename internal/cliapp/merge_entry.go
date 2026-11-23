package cliapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

// gitMergeCommandName is the command git runs as sdd's merge drivers; the
// root Before hook keeps it free of anything touching the repository, since
// git runs it in the middle of a merge.
const gitMergeCommandName = "git-merge"

// Any failure but a written conflict exits 2, which the driver command answers
// with git's text merge.
const mergeEntryFailureExit = 2

func gitMergeCmd() *cli.Command {
	return &cli.Command{
		Name:     gitMergeCommandName,
		Usage:    "Merge files as git's merge drivers (set up by sdd init and the sdd-owned graph clone)",
		Hidden:   true,
		Commands: []*cli.Command{mergeEntryCmd(), mergeConfigCmd()},
	}
}

func mergeEntryCmd() *cli.Command {
	return &cli.Command{
		Name:      "entry",
		Usage:     "Merge a graph entry file as git's merge driver",
		ArgsUsage: "<base> <ours> <theirs> [<marker-size> [<path>]]",
		Hidden:    true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			err := runMergeEntry(ctx, cmd)
			switch {
			case err == nil:
				return nil
			case errors.Is(err, handlers.ErrEntryConflict):
				return cli.Exit(err, git.MergeEntryConflictExit)
			default:
				return cli.Exit(err, mergeEntryFailureExit)
			}
		},
	}
}

func mergeConfigCmd() *cli.Command {
	return &cli.Command{
		Name:      "config",
		Usage:     "Merge .sdd/config.yaml per key as git's merge driver in the sdd-owned graph clone",
		ArgsUsage: "<base> <ours> <theirs> [<path>]",
		Hidden:    true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 3 || len(args) > 4 {
				return cli.Exit(fmt.Sprintf("git-merge config takes base, ours and theirs, then optionally the path; got %d arguments", len(args)), 1)
			}
			h := handlers.New(handlers.Options{Stderr: cmd.ErrWriter})
			err := h.MergeConfig(ctx, &command.MergeConfigCmd{
				BasePath: args[0], OursPath: args[1], TheirsPath: args[2],
				OnNotice: func(notice string) { fmt.Fprintln(cmd.ErrWriter, model.MergeNoticePrefix+notice) },
			})
			if err != nil {
				return cli.Exit(err, 1)
			}
			return nil
		},
	}
}

func runMergeEntry(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) < 3 || len(args) > 5 {
		return fmt.Errorf("git-merge entry takes base, ours and theirs, then optionally the marker size and path; got %d arguments", len(args))
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
