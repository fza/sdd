package cliapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// dismissCmd sets entries a session's capture read aside with a code, into
// the same verdicts the engine capture reports (20261109-004506-d-cpt-udz,
// D9).
func dismissCmd() *cli.Command {
	return &cli.Command{
		Name:      "dismiss",
		Usage:     "Set entries a session's capture read aside, with the code naming why",
		ArgsUsage: "<entry-id...>",
		Flags: append(sessionFlags(),
			&cli.StringFlag{Name: "because", Usage: "Why the entries are set aside: " + strings.Join(model.GroundingVerdictCodes(), ", ")},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			ids := cmd.Args().Slice()
			if len(ids) == 0 {
				return fmt.Errorf("name at least one entry to dismiss")
			}
			if cmd.String("session") == "" {
				return fmt.Errorf("sdd dismiss needs --session: verdicts belong to a capture in an open session")
			}
			if cmd.String("because") == "" {
				return fmt.Errorf("--because is required: %s", strings.Join(model.GroundingVerdictCodes(), ", "))
			}
			session, err := joinSession(ctx, cmd, "")
			if err != nil {
				return err
			}
			instance, err := session.capture(cmd, true)
			if err != nil {
				return err
			}
			serve, err := session.workflow.Dismiss(ctx, session.identity, sdd.WorkflowDismissRequest{Instance: instance, IDs: ids, Code: cmd.String("because")})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.Writer, "Dismissed %s as %s in capture %s (step %s)\n", strings.Join(ids, ", "), cmd.String("because"), instance, serve.Step)
			for _, diagnostic := range serve.Diagnostics {
				fmt.Fprintf(cmd.Writer, "  %s\n", diagnostic)
			}
			return nil
		},
	}
}
