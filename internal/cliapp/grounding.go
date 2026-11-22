package cliapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cliout"
	clitui "github.com/networkteam/sdd/internal/cliout/tui"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// groundingCmd reports and maintains the prefilter's calibration.
func groundingCmd() *cli.Command {
	return &cli.Command{
		Name:  "grounding",
		Usage: "Inspect and update the grounding prefilter's calibration",
		Commands: []*cli.Command{
			{
				Name:  "status",
				Usage: "Show the labels, the ranking judge's calibration and the hiding rules' steps, proofs and misses",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "exclude-replay", Usage: "Calibrate on the labels of live captures alone, leaving out those seeded from replayed ones"},
					&cli.BoolFlag{Name: "verbose", Usage: "Show the ranking judge's full calibration: label weights and kinds, thresholds, ranking recall, drift steps, opened hidden entries and precision per reason code"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					application, project, identity, err := workingApplication(ctx, cmd)
					if err != nil {
						return err
					}
					status, err := application.GroundingStatus(ctx, identity, project, sdd.GroundingStatusRequest{ExcludeReplay: cmd.Bool("exclude-replay"), Verbose: cmd.Bool("verbose")})
					if err != nil {
						return err
					}
					_, err = fmt.Fprintln(cmd.Writer, status.Text)
					return err
				},
			},
			{
				Name:  "update",
				Usage: "Label each entry with every entry it refs, closes or supersedes, then have the ranking judge score the labelled pairs it has not scored, one request per referencing entry",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "force", Usage: "Score every labelled pair again, replacing the ranking judge's scores"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					application, project, identity, err := workingApplication(ctx, cmd)
					if err != nil {
						return err
					}
					return update(ctx, cmd, application, project, identity)
				},
			},
		},
	}
}

// update seeds the graph's own references and has the ranking judge score
// the labelled pairs. A failed subject does not stop the others: the
// failures are listed once the judge is done, and fail the command.
func update(ctx context.Context, cmd *cli.Command, application *sdd.Application, project sdd.ProjectID, identity sdd.RequestIdentity) error {
	request := sdd.GroundingUpdateRequest{Force: cmd.Bool("force")}
	work := func(ctx context.Context) (sdd.GroundingUpdate, error) {
		return application.UpdateGrounding(ctx, identity, project, request)
	}
	var updated sdd.GroundingUpdate
	var err error
	if cliout.IsInteractive(cmd.ErrWriter) {
		progress := newRescoreProgress(&request)
		updated, err = clitui.Interactive(ctx, loggerViewPolicy(ctx),
			clitui.View{Reader: cmd.Reader, Writer: cmd.ErrWriter, Progress: progress, StreamLogs: true}, work)
	} else {
		updated, err = work(ctx)
	}
	if err != nil {
		return err
	}
	seeded, scored := updated.Seeded, updated.Scored
	_, err = fmt.Fprintf(cmd.Writer, "Seeded %d replayed reference labels from the graph's references; %d pairs already labelled\n", seeded.Added, seeded.Skipped)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.Writer, "%s scored %d labelled pairs in %d requests; %d name an entry the graph no longer holds; %d too long to score; %d left unscored by a failed request\n", scored.Judge, scored.Scored, scored.Requests, scored.Missing, scored.Unanswered, scored.Failed)
	if err != nil {
		return err
	}
	var failures []string
	for _, failure := range scored.Failures {
		failures = append(failures, fmt.Sprintf("  %s %s: %v\n", scored.Judge, failure.Subject, failure.Err))
	}
	if len(failures) == 0 {
		return nil
	}
	_, err = fmt.Fprintf(cmd.ErrWriter, "failed to score, their pairs stay unscored for the next update:\n%s", strings.Join(failures, ""))
	if err != nil {
		return err
	}
	return fmt.Errorf("%d subjects failed to score", len(failures))
}

// workingApplication builds the application over the project the working
// directory belongs to.
func workingApplication(ctx context.Context, cmd *cli.Command) (*sdd.Application, sdd.ProjectID, sdd.RequestIdentity, error) {
	return workingApplicationTimed(ctx, cmd, "")
}

// workingApplicationTimed is workingApplication with its LLM runner bounded
// by the duration flag timeoutFlag names.
func workingApplicationTimed(ctx context.Context, cmd *cli.Command, timeoutFlag string) (*sdd.Application, sdd.ProjectID, sdd.RequestIdentity, error) {
	served, err := buildServedProject(ctx, cmd, timeoutFlag, newConfigFiles(), nil)
	if err != nil {
		return nil, "", sdd.RequestIdentity{}, err
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: served.access, Sessions: served.sessions, StagedBlobs: served.blobs})
	if err != nil {
		return nil, "", sdd.RequestIdentity{}, err
	}
	return application, served.access.runtime.Project().ID, sdd.RequestIdentity{Subject: "local"}, nil
}
