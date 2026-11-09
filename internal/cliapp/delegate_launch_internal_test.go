package cliapp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

func launcherServing(t *testing.T, cfg *model.PerRepoConfig) headlessLauncher {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	project := &servedProject{access: &localRuntimeAccess{project: "served"}, cfg: cfg, layout: meta.Project{Root: t.TempDir(), WorkRoot: t.TempDir()}}
	return headlessLauncher{watch: &configWatch{current: project}}
}

func TestHeadlessLauncher_LeavesAnUnconfiguredLaunchToTheHarness(t *testing.T) {
	launcher := launcherServing(t, &model.PerRepoConfig{})

	_, err := launcher.LaunchDelegate(context.Background(), sdd.DelegateLaunch{Project: "served", Round: 1})

	if !errors.Is(err, sdd.ErrDelegateNotLaunched) {
		t.Fatalf("a delegate without a configured launch should go back to the harness, got %v", err)
	}
}

func TestHeadlessLauncher_LaunchesOnlyInTheServedProject(t *testing.T) {
	launcher := launcherServing(t, &model.PerRepoConfig{})

	_, err := launcher.LaunchDelegate(context.Background(), sdd.DelegateLaunch{Project: "dependency", Launch: "claude", Round: 1})

	if err == nil || !strings.Contains(err.Error(), "only in the served project served, not in dependency") {
		t.Fatalf("a launch in another project should be refused, got %v", err)
	}
}

func TestHeadlessLauncher_FallsBackThroughTheProjectsAgents(t *testing.T) {
	cases := map[string]struct {
		agents  []model.AgentTarget
		refusal string
	}{
		"named agents":      {agents: []model.AgentTarget{model.AgentCodex, model.AgentClaude}, refusal: "supported_agents (codex, claude)"},
		"default to claude": {refusal: "supported_agents (claude)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			launcher := launcherServing(t, &model.PerRepoConfig{SupportedAgents: tc.agents})

			_, err := launcher.LaunchDelegate(context.Background(), sdd.DelegateLaunch{Project: "served", Launch: sdd.DelegateLaunchFallback, Round: 1})

			if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("the fallback should try the project's supported agents in order, got %v", err)
			}
		})
	}
}

func TestHeadlessLauncher_RunsTheConfiguredDriver(t *testing.T) {
	driver := filepath.Join(t.TempDir(), "absent-driver")
	launcher := launcherServing(t, &model.PerRepoConfig{BaseConfig: model.BaseConfig{Verifier: model.VerifierConfig{Launch: model.VerifierLaunchCustomDriver, Driver: model.VerifierDriver{Command: driver}}}})

	_, err := launcher.LaunchDelegate(context.Background(), sdd.DelegateLaunch{Project: "served", Launch: "custom_driver", Round: 1})

	if err == nil || !strings.Contains(err.Error(), driver+" failed") {
		t.Fatalf("the launch should run the driver the config names, got %v", err)
	}
}
