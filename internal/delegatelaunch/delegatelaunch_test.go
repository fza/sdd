package delegatelaunch_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/delegatelaunch"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

const roundPrompt = "Call next with session child-session, instance verify-1 and no report."

func settings(t *testing.T, launch string) delegatelaunch.Settings {
	t.Helper()
	workDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return delegatelaunch.Settings{Runner: launch, Agents: []string{"claude"}, WorkDir: workDir, SDD: "/opt/sdd/bin/sdd"}
}

func firstRound() delegatelaunch.Round {
	return delegatelaunch.Round{Session: "child-session", Instance: "verify-1", Number: 1, Prompt: roundPrompt}
}

func launch(t *testing.T, launchSettings delegatelaunch.Settings, round delegatelaunch.Round) delegatelaunch.Launched {
	t.Helper()
	launched, err := delegatelaunch.Run(context.Background(), launchSettings, round)
	if err != nil {
		t.Fatal(err)
	}
	return launched
}

func refused(t *testing.T, launchSettings delegatelaunch.Settings, round delegatelaunch.Round, refusal string) {
	t.Helper()
	_, err := delegatelaunch.Run(context.Background(), launchSettings, round)
	if err == nil || !strings.Contains(err.Error(), refusal) {
		t.Fatalf("the launch should be refused with %q, got %v", refusal, err)
	}
}

func TestRun_ClaudeRunsHeadlessWithTheReadOnlyToolSet(t *testing.T) {
	agents := test.FakeAgents(t, "claude")
	test.AgentAnswers(t, agents, "claude", `{"type":"result","is_error":false,"session_id":"claude-7","result":"done"}`, "", 0)
	launchSettings := settings(t, "claude")

	launched := launch(t, launchSettings, firstRound())

	if launched != (delegatelaunch.Launched{Session: "claude-7", Runner: "claude", Message: "done"}) {
		t.Fatalf("the round should report claude's session and last message, got %+v", launched)
	}
	call := test.LastAgentCall(t, agents, "claude")
	if call.Dir != launchSettings.WorkDir {
		t.Fatalf("the agent should work in the project checkout, got %s", call.Dir)
	}
	if call.Stdin != roundPrompt {
		t.Fatalf("the prompt should go on stdin, got %q", call.Stdin)
	}
	want := []string{
		"-p", "--output-format", "json", "--setting-sources", "", "--disable-slash-commands",
		"--tools", "Read,Grep,Glob,WebFetch,WebSearch",
		"--allowedTools", "Read,Grep,Glob,WebFetch,WebSearch,mcp__sdd__*",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{"sdd":{"args":["serve"],"command":"/opt/sdd/bin/sdd","type":"stdio"}}}`,
	}
	if !slices.Equal(call.Args, want) {
		t.Fatalf("claude should run with the read-only tools, no shell, and sdd as its only MCP server, got %q", call.Args)
	}
}

func TestRun_ClaudeResumesThePreviousRound(t *testing.T) {
	agents := test.FakeAgents(t, "claude")
	test.AgentAnswers(t, agents, "claude", `{"is_error":false,"session_id":"claude-7"}`, "", 0)
	round := firstRound()
	round.Number, round.Resume = 2, "claude-7"

	launch(t, settings(t, "claude"), round)

	args := test.LastAgentCall(t, agents, "claude").Args
	if !slices.Equal(args[len(args)-2:], []string{"--resume", "claude-7"}) {
		t.Fatalf("a later round should resume the reported session, got %q", args)
	}
}

func TestRun_ClaudeFailures(t *testing.T) {
	cases := map[string]struct {
		stdout, stderr string
		exit           int
		refusal        string
	}{
		"error result":  {stdout: `{"is_error":true,"session_id":"claude-7","result":"budget exceeded"}`, refusal: "claude ended the round with an error: budget exceeded"},
		"no session id": {stdout: `{"is_error":false}`, refusal: "claude reported no session_id to resume"},
		"no JSON":       {stdout: "plain words", refusal: "claude answered no JSON result"},
		"nonzero exit":  {stderr: "not logged in", exit: 1, refusal: "claude failed: exit status 1: not logged in"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			agents := test.FakeAgents(t, "claude")
			test.AgentAnswers(t, agents, "claude", tc.stdout, tc.stderr, tc.exit)

			refused(t, settings(t, "claude"), firstRound(), tc.refusal)
		})
	}
}

func TestRun_CustomDriverContract(t *testing.T) {
	agents := test.FakeAgents(t, "driver")
	test.AgentAnswers(t, agents, "driver", "starting\n"+`{"driver_state":"pane-4"}`, "", 0)
	launchSettings := settings(t, "driver")
	launchSettings.Driver = model.VerifierDriver{Command: filepath.Join(agents, "driver"), Args: []string{"--role", "verifier"}}
	round := firstRound()
	round.Number, round.Resume = 2, "pane-3"

	launched := launch(t, launchSettings, round)

	if launched != (delegatelaunch.Launched{Session: "pane-4", Runner: "driver"}) {
		t.Fatalf("the round should keep the driver's state, got %+v", launched)
	}
	call := test.LastAgentCall(t, agents, "driver")
	if !slices.Equal(call.Args, []string{"--role", "verifier"}) {
		t.Fatalf("the driver should run with its configured args, got %q", call.Args)
	}
	var request delegatelaunch.DriverRequest
	err := json.Unmarshal([]byte(call.Stdin), &request)
	if err != nil {
		t.Fatal(err)
	}
	want := delegatelaunch.DriverRequest{Round: 2, Session: "child-session", Instance: "verify-1", Prompt: roundPrompt, DriverState: "pane-3"}
	if request != want {
		t.Fatalf("the driver should read the round's request on stdin, got %+v", request)
	}
}

func TestRun_CustomDriverFailures(t *testing.T) {
	cases := map[string]struct {
		stdout  string
		exit    int
		refusal string
	}{
		"nonzero exit":    {exit: 3, refusal: "exit status 3"},
		"no driver state": {stdout: `{"state":"pane-4"}`, refusal: `the driver printed no {"driver_state": ...} line`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			agents := test.FakeAgents(t, "driver")
			test.AgentAnswers(t, agents, "driver", tc.stdout, "", tc.exit)
			launchSettings := settings(t, "driver")
			launchSettings.Driver = model.VerifierDriver{Command: filepath.Join(agents, "driver")}

			refused(t, launchSettings, firstRound(), tc.refusal)
		})
	}
}

func TestRun_CustomDriverNeedsACommand(t *testing.T) {
	test.FakeAgents(t)

	refused(t, settings(t, "driver"), firstRound(), "verify.runner driver needs verify.driver.command")
}

func TestRun_ARoundPastItsTimeoutFails(t *testing.T) {
	agents := test.FakeAgents(t, "claude")
	test.AgentHangs(t, agents, "claude")
	launchSettings := settings(t, "claude")
	launchSettings.Timeout = 200 * time.Millisecond

	refused(t, launchSettings, firstRound(), "claude did not end its round in time")
}

func TestRun_FallbackTakesClaudeWhereSupportedAndFound(t *testing.T) {
	cases := map[string]struct {
		installed []string
		agents    []string
		want      string
	}{
		"skips codex":           {installed: []string{"claude", "codex"}, agents: []string{"codex", "claude"}, want: "claude"},
		"skips no headless cli": {installed: []string{"claude", "gemini"}, agents: []string{"gemini", "claude"}, want: "claude"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			agents := test.FakeAgents(t, tc.installed...)
			for _, agent := range tc.installed {
				test.AgentAnswers(t, agents, agent, `{"session_id":"s-1"}`, "", 0)
			}
			launchSettings := settings(t, delegatelaunch.Fallback)
			launchSettings.Agents = tc.agents

			launched := launch(t, launchSettings, firstRound())

			if launched.Runner != tc.want {
				t.Fatalf("the fallback should take claude where supported_agents names it and its CLI is found, got %q", launched.Runner)
			}
		})
	}
}

func TestRun_FallbackWithNoSupportedCLIFoundIsRefused(t *testing.T) {
	test.FakeAgents(t, "gemini", "codex")
	launchSettings := settings(t, delegatelaunch.Fallback)
	launchSettings.Agents = []string{"claude", "codex"}

	refused(t, launchSettings, firstRound(), "no CLI of supported_agents (claude, codex) is on PATH: set verify.runner")
}

func TestRun_SubagentAndCodexAreNoHeadlessLaunch(t *testing.T) {
	for _, runner := range []string{"subagent", "codex"} {
		t.Run(runner, func(t *testing.T) {
			test.FakeAgents(t, runner)

			refused(t, settings(t, runner), firstRound(), `verify.runner "`+runner+`" is no headless runner`)
		})
	}
}

// Every runner hands Started the PID of the CLI it started.
func TestRun_HandsStartedTheCLIsPID(t *testing.T) {
	answers := map[string]string{
		"claude": `{"is_error":false,"session_id":"claude-7"}`,
		"driver": `{"driver_state":"pane-4"}`,
	}
	for runner, answer := range answers {
		t.Run(runner, func(t *testing.T) {
			agents := test.FakeAgents(t, runner)
			test.AgentAnswers(t, agents, runner, answer, "", 0)
			launchSettings := settings(t, runner)
			launchSettings.Driver = model.VerifierDriver{Command: filepath.Join(agents, "driver")}
			round := firstRound()
			var pids []int
			round.Started = func(pid int) error {
				pids = append(pids, pid)
				return nil
			}

			launch(t, launchSettings, round)

			if call := test.LastAgentCall(t, agents, runner); len(pids) != 1 || pids[0] != call.PID {
				t.Fatalf("Started should get the PID of the CLI the round ran once, got %v for %d", pids, call.PID)
			}
		})
	}
}

// A Started that fails stops the CLI it was handed and fails the round.
func TestRun_AFailingStartedStopsTheCLI(t *testing.T) {
	agents := test.FakeAgents(t, "claude")
	test.AgentHangs(t, agents, "claude")
	round := firstRound()
	var pid int
	round.Started = func(started int) error {
		pid = started
		return errors.New("the child ended")
	}

	began := time.Now()
	refused(t, settings(t, "claude"), round, "recording the claude process: the child ended")

	if time.Since(began) > 5*time.Second {
		t.Fatal("the round should stop the CLI at once, not wait for it to end")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("the CLI should be stopped, got %v", err)
	}
}

// The headless claude runs with the model and effort verify.claude sets, and
// a resumed round keeps them.
func TestRun_HeadlessClaudeTakesItsModelAndEffort(t *testing.T) {
	agents := test.FakeAgents(t, "claude")
	test.AgentAnswers(t, agents, "claude", `{"type":"result","is_error":false,"session_id":"claude-7","result":"done"}`, "", 0)
	launchSettings := settings(t, "claude")
	launchSettings.Claude = model.VerifierAgent{Model: "opus", Effort: "xhigh"}
	round := firstRound()
	round.Resume = "claude-6"

	launch(t, launchSettings, round)

	args := test.LastAgentCall(t, agents, "claude").Args
	want := []string{"--model", "opus", "--effort", "xhigh", "--resume", "claude-6"}
	if !slices.Equal(args[len(args)-len(want):], want) {
		t.Fatalf("the launch should pass its model and effort before the resume, got %q", args)
	}
}

// A launch whose block sets nothing runs with the CLI's own model and effort.
func TestRun_HeadlessClaudeWithoutABlockPassesNoModel(t *testing.T) {
	agents := test.FakeAgents(t, "claude")
	test.AgentAnswers(t, agents, "claude", `{"type":"result","is_error":false,"session_id":"claude-7","result":"done"}`, "", 0)

	launch(t, settings(t, "claude"), firstRound())

	for _, arg := range test.LastAgentCall(t, agents, "claude").Args {
		if arg == "--model" || arg == "--effort" {
			t.Fatalf("a launch without a block should pass no model or effort, got %q", test.LastAgentCall(t, agents, "claude").Args)
		}
	}
}
