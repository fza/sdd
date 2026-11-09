package delegatelaunch_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
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
	return delegatelaunch.Settings{Launch: launch, Agents: []string{"claude", "codex"}, WorkDir: workDir, SDD: "/opt/sdd/bin/sdd"}
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

	if launched != (delegatelaunch.Launched{Session: "claude-7", Launch: "claude"}) {
		t.Fatalf("the round should report claude's session, got %+v", launched)
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
		"--tools", "Read,Grep,Glob,WebFetch,WebSearch,Bash",
		"--allowedTools", "Read,Grep,Glob,WebFetch,WebSearch,mcp__sdd__*,Bash(git log:*),Bash(git show:*),Bash(git diff:*),Bash(git blame:*)",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{"sdd":{"args":["serve"],"command":"/opt/sdd/bin/sdd","type":"stdio"}}}`,
	}
	if !slices.Equal(call.Args, want) {
		t.Fatalf("claude should run with the read-only flag set and sdd as its only MCP server, got %q", call.Args)
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

func TestRun_CodexRunsInAReadOnlySandbox(t *testing.T) {
	agents := test.FakeAgents(t, "codex")
	events := strings.Join([]string{
		`{"type":"thread.started","thread_id":"codex-3"}`,
		`{"type":"turn.started"}`,
		`{"type":"turn.completed"}`,
	}, "\n")
	test.AgentAnswers(t, agents, "codex", events, "", 0)
	launchSettings := settings(t, "codex")

	launched := launch(t, launchSettings, firstRound())

	if launched != (delegatelaunch.Launched{Session: "codex-3", Launch: "codex"}) {
		t.Fatalf("the round should report codex's thread, got %+v", launched)
	}
	call := test.LastAgentCall(t, agents, "codex")
	if call.Dir != launchSettings.WorkDir {
		t.Fatalf("the agent should work in the project checkout, got %s", call.Dir)
	}
	want := []string{
		"exec", "--json", "--sandbox", "read-only",
		"-c", "project_doc_max_bytes=0",
		"-c", `mcp_servers.sdd.command="/opt/sdd/bin/sdd"`,
		"-c", `mcp_servers.sdd.args=["serve"]`,
		roundPrompt,
	}
	if !slices.Equal(call.Args, want) {
		t.Fatalf("codex should run read-only with sdd as an MCP server and the prompt last, got %q", call.Args)
	}
}

func TestRun_CodexResumesThePreviousRound(t *testing.T) {
	agents := test.FakeAgents(t, "codex")
	test.AgentAnswers(t, agents, "codex", `{"type":"turn.completed"}`, "", 0)
	round := firstRound()
	round.Number, round.Resume = 2, "codex-3"

	launched := launch(t, settings(t, "codex"), round)

	if launched.Session != "codex-3" {
		t.Fatalf("a resumed round should keep the session when codex reports none, got %q", launched.Session)
	}
	args := test.LastAgentCall(t, agents, "codex").Args
	if !slices.Equal(args[len(args)-3:], []string{"resume", "codex-3", roundPrompt}) {
		t.Fatalf("a later round should resume the session before the prompt, got %q", args)
	}
}

func TestRun_CodexErrorEventFailsTheRound(t *testing.T) {
	agents := test.FakeAgents(t, "codex")
	test.AgentAnswers(t, agents, "codex", `{"type":"thread.started","thread_id":"codex-3"}`+"\n"+`{"type":"error","message":"stream disconnected"}`, "", 0)

	refused(t, settings(t, "codex"), firstRound(), "codex ended the round with an error: stream disconnected")
}

func TestRun_CustomDriverContract(t *testing.T) {
	agents := test.FakeAgents(t, "driver")
	test.AgentAnswers(t, agents, "driver", "starting\n"+`{"driver_state":"pane-4"}`, "", 0)
	launchSettings := settings(t, "custom_driver")
	launchSettings.Driver = model.VerifierDriver{Command: filepath.Join(agents, "driver"), Args: []string{"--role", "verifier"}}
	round := firstRound()
	round.Number, round.Resume = 2, "pane-3"

	launched := launch(t, launchSettings, round)

	if launched != (delegatelaunch.Launched{Session: "pane-4", Launch: "custom_driver"}) {
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
			launchSettings := settings(t, "custom_driver")
			launchSettings.Driver = model.VerifierDriver{Command: filepath.Join(agents, "driver")}

			refused(t, launchSettings, firstRound(), tc.refusal)
		})
	}
}

func TestRun_CustomDriverNeedsACommand(t *testing.T) {
	test.FakeAgents(t)

	refused(t, settings(t, "custom_driver"), firstRound(), "verify.launch custom_driver needs verify.driver.command")
}

func TestRun_ARoundPastItsTimeoutFails(t *testing.T) {
	agents := test.FakeAgents(t, "claude")
	test.AgentHangs(t, agents, "claude")
	launchSettings := settings(t, "claude")
	launchSettings.Timeout = 200 * time.Millisecond

	refused(t, launchSettings, firstRound(), "claude did not end its round in time")
}

func TestRun_FallbackTakesTheFirstSupportedAgentFound(t *testing.T) {
	cases := map[string]struct {
		installed []string
		agents    []string
		want      string
	}{
		"first in order":        {installed: []string{"claude", "codex"}, agents: []string{"codex", "claude"}, want: "codex"},
		"skips one not found":   {installed: []string{"codex"}, agents: []string{"claude", "codex"}, want: "codex"},
		"skips no headless cli": {installed: []string{"claude", "gemini"}, agents: []string{"gemini", "claude"}, want: "claude"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			agents := test.FakeAgents(t, tc.installed...)
			for _, agent := range tc.installed {
				test.AgentAnswers(t, agents, agent, `{"session_id":"s-1","thread_id":"s-1"}`, "", 0)
			}
			launchSettings := settings(t, delegatelaunch.Fallback)
			launchSettings.Agents = tc.agents

			launched := launch(t, launchSettings, firstRound())

			if launched.Launch != tc.want {
				t.Fatalf("the fallback should take the first supported agent whose CLI is found, got %q", launched.Launch)
			}
		})
	}
}

func TestRun_FallbackWithNoSupportedCLIFoundIsRefused(t *testing.T) {
	test.FakeAgents(t, "gemini")
	launchSettings := settings(t, delegatelaunch.Fallback)
	launchSettings.Agents = []string{"claude", "codex"}

	refused(t, launchSettings, firstRound(), "no CLI of supported_agents (claude, codex) is on PATH: set verify.launch")
}

func TestRun_SubagentIsNoHeadlessLaunch(t *testing.T) {
	test.FakeAgents(t)

	refused(t, settings(t, "subagent"), firstRound(), `verify.launch "subagent" is no headless launch`)
}

func TestRun_CodexReportingNoSessionFailsTheFirstRound(t *testing.T) {
	agents := test.FakeAgents(t, "codex")
	test.AgentAnswers(t, agents, "codex", `{"type":"turn.completed"}`, "", 0)

	refused(t, settings(t, "codex"), firstRound(), "codex reported no session to resume")
}

// Each headless launch runs with the model and effort its own block sets
// (verify.claude, verify.codex), in the CLI's own flags, and a resumed
// round keeps them.
func TestRun_HeadlessLaunchesTakeTheirModelAndEffort(t *testing.T) {
	codexEvents := `{"type":"thread.started","thread_id":"codex-3"}` + "\n" + `{"type":"turn.completed"}`
	for _, tc := range []struct {
		launch string
		answer string
		want   []string
	}{
		{"claude", `{"type":"result","is_error":false,"session_id":"claude-7","result":"done"}`, []string{"--model", "opus", "--effort", "xhigh", "--resume", "claude-6"}},
		{"codex", codexEvents, []string{"-m", "gpt-5.5", "-c", `model_reasoning_effort="high"`, "resume", "codex-2", roundPrompt}},
	} {
		t.Run(tc.launch, func(t *testing.T) {
			agents := test.FakeAgents(t, tc.launch)
			test.AgentAnswers(t, agents, tc.launch, tc.answer, "", 0)
			launchSettings := settings(t, tc.launch)
			launchSettings.Claude = model.VerifierAgent{Model: "opus", Effort: "xhigh"}
			launchSettings.Codex = model.VerifierAgent{Model: "gpt-5.5", Effort: "high"}
			round := firstRound()
			round.Resume = map[string]string{"claude": "claude-6", "codex": "codex-2"}[tc.launch]

			launch(t, launchSettings, round)

			args := test.LastAgentCall(t, agents, tc.launch).Args
			if !slices.Equal(args[len(args)-len(tc.want):], tc.want) {
				t.Fatalf("the launch should pass its own model and effort before the resume, got %q", args)
			}
		})
	}
}

// A launch whose block sets nothing runs with the CLI's own model and effort.
func TestRun_HeadlessLaunchesWithoutABlockPassNoModel(t *testing.T) {
	for _, launchName := range []string{"claude", "codex"} {
		t.Run(launchName, func(t *testing.T) {
			agents := test.FakeAgents(t, launchName)
			answer := `{"type":"result","is_error":false,"session_id":"claude-7","result":"done"}`
			if launchName == "codex" {
				answer = `{"type":"thread.started","thread_id":"codex-3"}`
			}
			test.AgentAnswers(t, agents, launchName, answer, "", 0)
			launchSettings := settings(t, launchName)
			launchSettings.Claude = model.VerifierAgent{Model: "opus"}
			if launchName == "claude" {
				launchSettings.Claude = model.VerifierAgent{}
				launchSettings.Codex = model.VerifierAgent{Model: "gpt-5.5"}
			}

			launch(t, launchSettings, firstRound())

			for _, arg := range test.LastAgentCall(t, agents, launchName).Args {
				if arg == "--model" || arg == "-m" || arg == "--effort" || strings.HasPrefix(arg, "model_reasoning_effort") {
					t.Fatalf("a launch should not take another launch's model or effort, got %q", test.LastAgentCall(t, agents, launchName).Args)
				}
			}
		})
	}
}
