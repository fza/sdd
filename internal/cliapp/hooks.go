package cliapp

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

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
				Usage: "Let the hooks in the project's committed config run on this machine, as they are now",
				Action: func(_ context.Context, cmd *cli.Command) error {
					locations, err := repos.DefaultLocations()
					if err != nil {
						return err
					}
					project, err := resolveProject()
					if err != nil {
						return err
					}
					allowed, err := hooks.Allow(hooks.Sources{GlobalConfig: locations.ConfigPath, Project: project}, hooks.NewAllows(locations.StateRoot))
					if err != nil {
						return err
					}
					if len(allowed) == 0 {
						_, err = fmt.Fprintln(cmd.Writer, "No committed hooks to allow.")
						return err
					}
					for _, hook := range allowed {
						_, err = fmt.Fprintf(cmd.Writer, "allowed %s %s (%s)\n", hook.Event, hook.Command, hook.Origin.ConfigPath)
						if err != nil {
							return err
						}
					}
					_, err = fmt.Fprintln(cmd.Writer, "A change to any of them needs `sdd hooks allow` again.")
					return err
				},
			},
		},
	}
}
