package cliapp

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

// recheckCmd shows the recheck list and scans the history for hand edits
// (20261109-004506-d-cpt-udz, D5).
func recheckCmd() *cli.Command {
	return &cli.Command{
		Name:  "recheck",
		Usage: "Show and update the recheck list: strains between entries and hand edits of committed entries",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "Show the items no later entry healed, with the healing suggestion",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					application, project, identity, err := workingApplication(ctx, cmd)
					if err != nil {
						return err
					}
					list, err := application.RecheckList(ctx, identity, project)
					if err != nil {
						return err
					}
					if list.Open == 0 {
						_, err = fmt.Fprintln(cmd.Writer, "No open recheck items.")
						return err
					}
					_, err = fmt.Fprintln(cmd.Writer, list.Text)
					return err
				},
			},
			{
				Name:  "scan",
				Usage: "Record every hand edit committed since the baseline; the first scan sets the baseline",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					application, project, identity, err := workingApplication(ctx, cmd)
					if err != nil {
						return err
					}
					found, err := application.ScanHandEdits(ctx, identity, project)
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(cmd.Writer, "Found %d hand edits since the baseline\n", found)
					return err
				},
			},
		},
	}
}
