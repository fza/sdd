package cliapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/roundproc"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// delegateRoundCommandName is the hidden command a launched round's detached
// process runs (20261114-160323-d-tac-n0n).
const delegateRoundCommandName = "delegate-round"

// roundProcesses runs launched rounds as detached `sdd delegate-round`
// processes in the directory and with the root flags of the command that
// composes it, so a round's process resolves the same project. Each writes
// its output and its info-level log to a file of its own under logs.
type roundProcesses struct {
	dir  string
	sdd  string
	root []string
	logs string
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
	logs, err := roundLogDir()
	if err != nil {
		return roundProcesses{}, err
	}
	root := []string{"--verbose"}
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
	return roundProcesses{dir: dir, sdd: executable, root: root, logs: logs}, nil
}

// roundLogDir is where the rounds of the working directory's repository log,
// in the state root, keyed like its relevance ledger.
func roundLogDir() (string, error) {
	sddDir, err := resolveSDDDir()
	if err != nil {
		return "", err
	}
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	locations, err := repos.DefaultLocations()
	if err != nil {
		return "", err
	}
	stableRepoRoot, err := git.StableRepoRoot(filepath.Dir(sddDir))
	if err != nil {
		return "", err
	}
	return filepath.Join(locations.StateRoot, roundproc.LogDirName, filepath.FromSlash(persistentIndexRepoKey(cfg, stableRepoRoot))), nil
}

func (p roundProcesses) Log(round sdd.LaunchedRound) string {
	return roundproc.LogPath(p.logs, string(round.Session), round.Number)
}

func (p roundProcesses) Start(_ context.Context, round sdd.LaunchedRound) (sdd.RoundProcess, error) {
	args := append(append([]string(nil), p.root...), delegateRoundCommandName,
		"--session", string(round.Session), "--instance", round.Instance, "--procedure", round.Procedure,
		"--project", string(round.Project), "--runner", round.Runner, "--round", strconv.Itoa(round.Number))
	process, err := roundproc.Start(p.dir, p.Log(round), p.sdd, args...)
	return sdd.RoundProcess(process), err
}

func (roundProcesses) Alive(_ context.Context, process sdd.RoundProcess) (bool, error) {
	return roundproc.Alive(roundproc.Process(process))
}

func (roundProcesses) Stop(_ context.Context, process, agent sdd.RoundProcess) error {
	return roundproc.Stop(roundproc.Process(process), roundproc.Process(agent))
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
