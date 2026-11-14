package cliapp

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/roundproc"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// delegateRoundCommandName is the hidden command a launched round's detached
// process runs (20261114-160323-d-tac-n0n).
const delegateRoundCommandName = "delegate-round"

// roundProcesses runs launched rounds as detached `sdd delegate-round`
// processes in the directory and with the root flags of the command that
// composes it, so a round's process resolves the same project.
type roundProcesses struct {
	dir  string
	sdd  string
	root []string
}

func newRoundProcesses(cmd *cli.Command) (roundProcesses, error) {
	executable, err := os.Executable()
	if err != nil {
		return roundProcesses{}, fmt.Errorf("locating the sdd executable for launched rounds: %w", err)
	}
	dir, err := os.Getwd()
	if err != nil {
		return roundProcesses{}, fmt.Errorf("getting working directory: %w", err)
	}
	var root []string
	if cmd.String("graph-dir") != "" {
		graphDir, err := resolveGraphDir(cmd)
		if err != nil {
			return roundProcesses{}, err
		}
		root = append(root, "--graph-dir", graphDir)
	}
	if localConfigOverride != "" {
		root = append(root, "--local-config", localConfigOverride)
	}
	return roundProcesses{dir: dir, sdd: executable, root: root}, nil
}

func (p roundProcesses) Start(_ context.Context, round sdd.LaunchedRound) (sdd.RoundProcess, error) {
	args := append(append([]string(nil), p.root...), delegateRoundCommandName,
		"--session", string(round.Session), "--instance", round.Instance, "--procedure", round.Procedure,
		"--project", string(round.Project), "--runner", round.Runner, "--round", strconv.Itoa(round.Number))
	process, err := roundproc.Start(p.dir, p.sdd, args...)
	return sdd.RoundProcess(process), err
}

func (roundProcesses) Alive(_ context.Context, process sdd.RoundProcess) (bool, error) {
	return roundproc.Alive(roundproc.Process(process))
}

func (roundProcesses) Stop(_ context.Context, process sdd.RoundProcess) error {
	return roundproc.Stop(roundproc.Process(process))
}

// delegateRoundCmd runs one launched round in the detached process sdd
// started for it, over the project serve composes.
func delegateRoundCmd() *cli.Command {
	return &cli.Command{
		Name:   delegateRoundCommandName,
		Usage:  "Run one delegate round sdd launched (started by sdd serve)",
		Hidden: true,
		Flags: append(embeddingFlags(),
			&cli.StringFlag{Name: "session", Required: true},
			&cli.StringFlag{Name: "instance", Required: true},
			&cli.StringFlag{Name: "procedure", Required: true},
			&cli.StringFlag{Name: "project", Required: true},
			&cli.StringFlag{Name: "runner", Required: true},
			&cli.IntFlag{Name: "round", Required: true},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			runner, err := newRunner(cmd, "")
			if err != nil {
				return err
			}
			watch, err := newConfigWatch(func() (string, error) { return resolveGraphDir(cmd) }, func() (*servedProject, error) {
				return buildServedProject(ctx, cmd, runner)
			})
			if err != nil {
				return err
			}
			application, identity, err := buildServedApplication(ctx, cmd, watch)
			if err != nil {
				return err
			}
			round := sdd.LaunchedRound{
				Session: sdd.SessionID(cmd.String("session")), Instance: cmd.String("instance"), Procedure: cmd.String("procedure"),
				Project: sdd.ProjectID(cmd.String("project")), Runner: cmd.String("runner"), Number: int(cmd.Int("round")),
			}
			return application.RunLaunchedRound(ctx, identity, round, os.Getpid())
		},
	}
}
