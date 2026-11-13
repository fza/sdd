package cliapp

import (
	"context"
	"fmt"
	"os"

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
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "exclude-replay", Usage: "Calibrate on the labels of live captures alone, leaving out those seeded from replayed ones"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					application, project, identity, err := workingApplication(ctx, cmd)
					if err != nil {
						return err
					}
					status, err := application.GroundingStatus(ctx, identity, project, sdd.GroundingStatusRequest{ExcludeReplay: cmd.Bool("exclude-replay")})
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
			{
				Name:  "seed",
				Usage: "Append a replayed reference label for each pair a JSON Lines file names as {\"a\": ID, \"b\": ID}, skipping pairs already labelled",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "from", Usage: "the JSON Lines file, or - for standard input", Required: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					store, err := workingRelevanceStore()
					if err != nil {
						return err
					}
					input := cmd.Reader
					if name := cmd.String("from"); name != "-" {
						file, err := os.Open(name)
						if err != nil {
							return err
						}
						defer file.Close()
						input = file
					}
					result, err := sdd.SeedReplayLabels(ctx, store, input)
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(cmd.Writer, "Seeded %d replayed reference labels; %d pairs already labelled\n", result.Added, result.Skipped)
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
