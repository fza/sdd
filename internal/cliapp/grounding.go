package cliapp

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// groundingCmd reports and maintains the prefilter's calibration
// (20261109-004506-d-cpt-udz, 7.3, D36).
func groundingCmd() *cli.Command {
	return &cli.Command{
		Name:  "grounding",
		Usage: "Inspect the grounding prefilter's calibration",
		Commands: []*cli.Command{
			{
				Name:  "status",
				Usage: "Show the current judge's labels, recall bound, thresholds, audit and drift state, and precision per reason code",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					application, project, identity, err := workingApplication(ctx, cmd)
					if err != nil {
						return err
					}
					status, err := application.GroundingStatus(ctx, identity, project)
					if err != nil {
						return err
					}
					_, err = fmt.Fprintln(cmd.Writer, status.Text)
					return err
				},
			},
			{
				Name:  "rescore",
				Usage: "Have the current judge score every labelled pair it has not scored, after a judge change",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					application, project, identity, err := workingApplication(ctx, cmd)
					if err != nil {
						return err
					}
					result, err := application.RescoreRelevance(ctx, identity, project)
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(cmd.Writer, "%s scored %d labelled pairs; %d name an entry the graph no longer holds; %d too long to score\n", result.Judge, result.Scored, result.Missing, result.Unanswered)
					return err
				},
			},
		},
	}
}

// workingApplication builds the application over the project the working
// directory belongs to.
func workingApplication(ctx context.Context, cmd *cli.Command) (*sdd.Application, sdd.ProjectID, sdd.RequestIdentity, error) {
	return workingApplicationTimed(ctx, cmd, "")
}

// workingApplicationTimed is workingApplication with its LLM runner bounded
// by the duration flag timeoutFlag names.
func workingApplicationTimed(ctx context.Context, cmd *cli.Command, timeoutFlag string) (*sdd.Application, sdd.ProjectID, sdd.RequestIdentity, error) {
	runner, err := newRunner(cmd, timeoutFlag)
	if err != nil {
		return nil, "", sdd.RequestIdentity{}, err
	}
	served, err := buildServedProject(ctx, cmd, runner)
	if err != nil {
		return nil, "", sdd.RequestIdentity{}, err
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: served.access, Sessions: served.sessions, StagedBlobs: served.blobs})
	if err != nil {
		return nil, "", sdd.RequestIdentity{}, err
	}
	return application, served.access.runtime.Project().ID, sdd.RequestIdentity{Subject: "local"}, nil
}
