package cliapp

import (
	"context"
	"fmt"
	"os"

	"github.com/networkteam/sdd/internal/delegatelaunch"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// headlessLauncher is the serve composition's DelegateLauncher: it runs the
// headless launch the project configures for a delegate, and leaves every
// other delegate to the harness.
type headlessLauncher struct {
	watch *configWatch
}

func (l headlessLauncher) LaunchDelegate(_ context.Context, launch sdd.DelegateLaunch) (sdd.DelegateRun, error) {
	if launch.Runner == "" {
		return sdd.DelegateRun{}, sdd.ErrDelegateNotLaunched
	}
	served := l.watch.served()
	if launch.Project != served.access.project {
		return sdd.DelegateRun{}, fmt.Errorf("sdd launches a headless delegate only in the served project %s, not in %s", served.access.project, launch.Project)
	}
	executable, err := os.Executable()
	if err != nil {
		return sdd.DelegateRun{}, fmt.Errorf("locating the sdd executable for the delegate's MCP server: %w", err)
	}
	settings := delegatelaunch.Settings{Runner: launch.Runner, WorkDir: served.layout.WorkRoot, SDD: executable}
	agents := model.DefaultSupportedAgents
	policy := model.VerifyConfig{}.VerifierPolicy()
	if served.cfg != nil {
		if len(served.cfg.SupportedAgents) > 0 {
			agents = served.cfg.SupportedAgents
		}
		policy = served.cfg.Verify.VerifierPolicy()
	}
	settings.Driver, settings.Claude, settings.Codex, settings.Timeout = policy.Driver, policy.Claude, policy.Codex, policy.RoundTimeout
	for _, agent := range agents {
		settings.Agents = append(settings.Agents, string(agent))
	}
	settings.Runner, err = delegatelaunch.Resolve(settings)
	if err != nil {
		return sdd.DelegateRun{}, err
	}
	round := func(ctx context.Context, round sdd.DelegateRound) (sdd.DelegateRoundEnd, error) {
		launched, err := delegatelaunch.Run(ctx, settings, delegatelaunch.Round{
			Session: string(launch.Session), Instance: launch.Instance, Number: round.Number, Prompt: round.Prompt, Resume: round.Resume,
		})
		return sdd.DelegateRoundEnd{Session: launched.Session, Message: launched.Message}, err
	}
	return sdd.DelegateRun{Runner: settings.Runner, Round: round}, nil
}
