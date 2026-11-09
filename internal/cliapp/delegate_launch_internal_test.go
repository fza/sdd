package cliapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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

	_, err := launcher.LaunchDelegate(context.Background(), sdd.DelegateLaunch{Project: "served"})

	if !errors.Is(err, sdd.ErrDelegateNotLaunched) {
		t.Fatalf("a delegate without a configured launch should go back to the harness, got %v", err)
	}
}

func TestHeadlessLauncher_LaunchesOnlyInTheServedProject(t *testing.T) {
	launcher := launcherServing(t, &model.PerRepoConfig{})

	_, err := launcher.LaunchDelegate(context.Background(), sdd.DelegateLaunch{Project: "dependency", Runner: "claude"})

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

			_, err := launcher.LaunchDelegate(context.Background(), sdd.DelegateLaunch{Project: "served", Runner: sdd.DelegateLaunchFallback})

			if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("the fallback should try the project's supported agents in order, got %v", err)
			}
		})
	}
}

func TestHeadlessLauncher_RunsTheConfiguredDriver(t *testing.T) {
	driver := filepath.Join(t.TempDir(), "absent-driver")
	launcher := launcherServing(t, &model.PerRepoConfig{BaseConfig: model.BaseConfig{Verify: model.VerifyConfig{Runner: model.VerifierRunnerDriver, Driver: model.VerifierDriver{Command: driver}}}})

	run, err := launcher.LaunchDelegate(context.Background(), sdd.DelegateLaunch{Project: "served", Runner: "driver"})
	if err != nil || run.Runner != "driver" {
		t.Fatalf("the launcher should take the driver runner on at once, got %+v, %v", run, err)
	}
	_, err = run.Round(context.Background(), sdd.DelegateRound{Number: 1})

	if err == nil || !strings.Contains(err.Error(), driver+" failed") {
		t.Fatalf("the launch should run the driver the config names, got %v", err)
	}
}

// The launcher hands each headless launch its own block of the served config:
// claude runs with verify.claude, never with verify.codex.
func TestHeadlessLauncher_PassesTheLaunchsModelAndEffort(t *testing.T) {
	cfg := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Verify: model.VerifyConfig{
		Runner: model.VerifierRunnerClaude,
		Claude: model.VerifierAgent{Model: "opus", Effort: "xhigh"},
		Codex:  model.VerifierAgent{Model: "gpt-5.5", Effort: "high"},
	}}}
	launcher := launcherServing(t, cfg)
	bin := t.TempDir()
	argsFile := filepath.Join(bin, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\ncat > /dev/null\necho '{\"type\":\"result\",\"is_error\":false,\"session_id\":\"claude-7\",\"result\":\"done\"}'\n"
	err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/bin:/usr/bin")

	run, err := launcher.LaunchDelegate(context.Background(), sdd.DelegateLaunch{Project: "served", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = run.Round(context.Background(), sdd.DelegateRound{Number: 1, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}

	recorded, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(recorded)), "\n")
	if !slices.Contains(args, "opus") || !slices.Contains(args, "xhigh") || slices.Contains(args, "gpt-5.5") {
		t.Fatalf("claude should run with verify.claude's model and effort only, got %q", args)
	}
}
