package cliapp

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/networkteam/sdd/internal/delegatelaunch"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/roundproc"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// headlessLauncher is the serve composition's DelegateLauncher: it runs the
// headless launch the project configures for a delegate, and leaves every
// other delegate to the harness. It reads the config sdd serve started with.
type headlessLauncher struct {
	project  sdd.ProjectID
	workRoot string
	cfg      *model.PerRepoConfig
}

func (l headlessLauncher) LaunchDelegate(_ context.Context, launch sdd.DelegateLaunch) (sdd.DelegateRun, error) {
	if launch.Runner == "" {
		return sdd.DelegateRun{}, sdd.ErrDelegateNotLaunched
	}
	if launch.Project != l.project {
		return sdd.DelegateRun{}, fmt.Errorf("sdd launches a headless delegate only in the served project %s, not in %s", l.project, launch.Project)
	}
	executable, err := os.Executable()
	if err != nil {
		return sdd.DelegateRun{}, fmt.Errorf("locating the sdd executable for the delegate's MCP server: %w", err)
	}
	settings := delegatelaunch.Settings{Runner: launch.Runner, WorkDir: l.workRoot, SDD: executable}
	agents := model.DefaultSupportedAgents
	policy := model.VerifyConfig{}.VerifierPolicy()
	if l.cfg != nil {
		if len(l.cfg.SupportedAgents) > 0 {
			agents = l.cfg.SupportedAgents
		}
		policy = l.cfg.Verify.VerifierPolicy()
	}
	settings.Driver, settings.Claude, settings.Timeout = policy.Driver, policy.Claude, policy.RoundTimeout
	for _, agent := range agents {
		settings.Agents = append(settings.Agents, string(agent))
	}
	settings.Runner, err = delegatelaunch.Resolve(settings)
	if err != nil {
		return sdd.DelegateRun{}, err
	}
	round := func(ctx context.Context, round sdd.DelegateRound) (sdd.DelegateRoundEnd, error) {
		var started func(pid int) error
		if round.AgentStarted != nil {
			started = func(pid int) error {
				agent, err := roundproc.Identify(pid)
				// An agent that ended at once left nothing running to stop.
				if errors.Is(err, roundproc.ErrEnded) {
					return nil
				}
				if err != nil {
					return err
				}
				return round.AgentStarted(ctx, sdd.RoundProcess(agent))
			}
		}
		launched, err := delegatelaunch.Run(ctx, settings, delegatelaunch.Round{
			Session: string(launch.Session), Instance: launch.Instance, Number: round.Number, Prompt: round.Prompt, Resume: round.Resume, Started: started,
		})
		return sdd.DelegateRoundEnd{Session: launched.Session, Message: launched.Message}, err
	}
	return sdd.DelegateRun{Runner: settings.Runner, Round: round}, nil
}
