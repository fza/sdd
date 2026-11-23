package cliapp

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/repos"
)

func hooksCmd() *cli.Command {
	return &cli.Command{
		Name:  "hooks",
		Usage: "Manage the hooks this project declares",
		Commands: []*cli.Command{
			{
				Name:  "allow",
				Usage: "Let the hooks that came with the project's repositories run on this machine, as they are now: those in .sdd/config.yaml, and in .sdd/config.local.yaml unless git says the repository does not track it",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					locations, err := repos.DefaultLocations()
					if err != nil {
						return err
					}
					project, err := resolveProject()
					if err != nil {
						return err
					}
					gated, err := hooks.Allow(ctx, hooks.Sources{GlobalConfig: locations.ConfigPath, Project: project, Tracker: git.TrackedFiles{}}, hooks.NewAllows(locations.StateRoot))
					if err != nil {
						return err
					}
					if len(gated) == 0 {
						_, err = fmt.Fprintln(cmd.Writer, "No hooks came with the repositories to allow.")
						return err
					}
					for _, hook := range gated {
						if hook.Err != nil {
							_, err = fmt.Fprintf(cmd.Writer, "refused %s %s: %v\n", hook.Event, hook.Command, hook.Err)
						} else {
							_, err = fmt.Fprintf(cmd.Writer, "allowed %s %s (%s)\n", hook.Event, hook.Command, hook.Origin.ConfigPath)
						}
						if err != nil {
							return err
						}
					}
					_, err = fmt.Fprintln(cmd.Writer, "A change to any of them, or to a file the repository tracks in .sdd/hooks/, needs `sdd hooks allow` again.")
					return err
				},
			},
		},
	}
}
