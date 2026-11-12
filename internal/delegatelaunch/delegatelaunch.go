// Package delegatelaunch runs the deep draft verifier's delegate without the
// drafting agent's harness (20261109-004729-d-cpt-xs5, V-D16 to V-D20): a
// headless claude or codex run per round, resumed by the session id the
// first round reported, or an external driver per round. The agent reaches
// the child session only through an sdd MCP server of its own; findings and
// answers travel through that server, never through the launch.
package delegatelaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// RoundTimeout bounds one launched round.
const RoundTimeout = 30 * time.Minute

// Fallback asks for the first headless runner of Settings.Agents whose CLI
// is found.
const Fallback = "fallback"

// Settings is how one project launches its deep verifier.
type Settings struct {
	// Runner is claude, codex, driver or Fallback.
	Runner string
	// Agents are the project's supported_agents, in order, for Fallback.
	Agents []string
	Driver model.VerifierDriver
	// Claude and Codex are the model and effort each headless runner runs
	// with; empty values leave the CLI's own.
	Claude model.VerifierAgent
	Codex  model.VerifierAgent
	// WorkDir is the project checkout the agent works in.
	WorkDir string
	// SDD is the sdd executable the agent's MCP server runs.
	SDD     string
	Timeout time.Duration
}

// Round is one round's launch.
type Round struct {
	Session  string
	Instance string
	Number   int
	Prompt   string
	// Resume is the session the previous round reported; empty on the first.
	Resume string
}

// Launched is what a round reports: the session to resume next round, the
// runner that ran, and the agent's last message, empty for a driver.
type Launched struct {
	Session string
	Runner  string
	Message string
}

// claudeTools are the tools a headless claude verifier may use (V-D18): read,
// search and glob, the web, the sdd MCP tools, and read-only git.
var claudeTools = []string{
	"Read", "Grep", "Glob", "WebFetch", "WebSearch", "mcp__sdd__*",
	"Bash(git log:*)", "Bash(git show:*)", "Bash(git diff:*)", "Bash(git blame:*)",
}

// Resolve names the runner Run runs: settings.Runner, or for Fallback the
// first agent of settings.Agents whose CLI is found.
func Resolve(settings Settings) (string, error) {
	if settings.Runner == Fallback {
		return fallback(settings.Agents)
	}
	return settings.Runner, nil
}

// Run launches one round and waits for it to end.
func Run(ctx context.Context, settings Settings, round Round) (Launched, error) {
	runner, err := Resolve(settings)
	if err != nil {
		return Launched{}, err
	}
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = RoundTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var launched Launched
	switch runner {
	case string(model.VerifierRunnerClaude):
		launched, err = runClaude(ctx, settings, round)
	case string(model.VerifierRunnerCodex):
		launched, err = runCodex(ctx, settings, round)
	case string(model.VerifierRunnerDriver):
		launched.Session, err = runDriver(ctx, settings, round)
	default:
		return Launched{}, fmt.Errorf("verify.runner %q is no headless runner", runner)
	}
	launched.Runner = runner
	return launched, err
}

func fallback(agents []string) (string, error) {
	for _, agent := range agents {
		if agent != string(model.VerifierRunnerClaude) && agent != string(model.VerifierRunnerCodex) {
			continue
		}
		_, err := exec.LookPath(agent)
		if err == nil {
			return agent, nil
		}
	}
	return "", fmt.Errorf("the harness has no subagent tool and no CLI of supported_agents (%s) is on PATH: set verify.runner to a runner this machine can run", strings.Join(agents, ", "))
}

// mcpConfig is the one MCP server a headless verifier reaches: sdd's own.
func mcpConfig(sdd string) (string, error) {
	encoded, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"sdd": map[string]any{"type": "stdio", "command": sdd, "args": []string{"serve"}}}})
	return string(encoded), err
}

// ClaudeArgs are the flags of a headless claude round (V-D18): print mode
// with a JSON result, no settings layer (so no project instructions, hooks or
// plugins) and no skills, the read-only tool set, and sdd as the only MCP
// server. The prompt goes on stdin. --safe-mode is no substitute: it drops the
// server --mcp-config passes too.
func ClaudeArgs(sdd, resume string, agent model.VerifierAgent) ([]string, error) {
	config, err := mcpConfig(sdd)
	if err != nil {
		return nil, err
	}
	args := []string{
		"-p", "--output-format", "json", "--setting-sources", "", "--disable-slash-commands",
		"--tools", "Read,Grep,Glob,WebFetch,WebSearch,Bash",
		"--allowedTools", strings.Join(claudeTools, ","),
		"--strict-mcp-config", "--mcp-config", config,
	}
	if agent.Model != "" {
		args = append(args, "--model", agent.Model)
	}
	if agent.Effort != "" {
		args = append(args, "--effort", agent.Effort)
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	return args, nil
}

func runClaude(ctx context.Context, settings Settings, round Round) (Launched, error) {
	args, err := ClaudeArgs(settings.SDD, round.Resume, settings.Claude)
	if err != nil {
		return Launched{}, err
	}
	out, err := run(ctx, settings.WorkDir, "claude", args, round.Prompt)
	if err != nil {
		return Launched{}, err
	}
	var result struct {
		SessionID string `json:"session_id"`
		IsError   bool   `json:"is_error"`
		Result    string `json:"result"`
	}
	err = json.Unmarshal(lastLine(out), &result)
	if err != nil {
		return Launched{}, fmt.Errorf("claude answered no JSON result: %w", err)
	}
	if result.IsError {
		return Launched{}, fmt.Errorf("claude ended the round with an error: %s", result.Result)
	}
	if result.SessionID == "" {
		return Launched{}, errors.New("claude reported no session_id to resume")
	}
	return Launched{Session: result.SessionID, Message: result.Result}, nil
}

// CodexArgs are the flags of a headless codex round (V-D18): exec with JSON
// events in a read-only sandbox, no project instructions (a zero byte budget
// for AGENTS.md), sdd as an MCP server whose tools run unasked, since exec's
// approval policy never asks and refuses every tool needing approval, and
// the round's prompt; a later round resumes the first round's session.
func CodexArgs(sdd, resume, prompt string, agent model.VerifierAgent) []string {
	args := []string{
		"exec", "--json", "--sandbox", "read-only",
		"-c", "project_doc_max_bytes=0",
		"-c", "mcp_servers.sdd.command=" + strconv.Quote(sdd),
		"-c", `mcp_servers.sdd.args=["serve"]`,
		"-c", `mcp_servers.sdd.default_tools_approval_mode="approve"`,
	}
	if agent.Model != "" {
		args = append(args, "-m", agent.Model)
	}
	if agent.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(agent.Effort))
	}
	if resume != "" {
		args = append(args, "resume", resume)
	}
	return append(args, prompt)
}

func runCodex(ctx context.Context, settings Settings, round Round) (Launched, error) {
	out, err := run(ctx, settings.WorkDir, "codex", CodexArgs(settings.SDD, round.Resume, round.Prompt, settings.Codex), "")
	if err != nil {
		return Launched{}, err
	}
	launched := Launched{Session: round.Resume}
	for _, line := range bytes.Split(out, []byte("\n")) {
		var event struct {
			Type      string `json:"type"`
			ThreadID  string `json:"thread_id"`
			SessionID string `json:"session_id"`
			Message   string `json:"message"`
			Item      struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Msg struct {
				Type      string `json:"type"`
				SessionID string `json:"session_id"`
				Message   string `json:"message"`
			} `json:"msg"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		switch {
		case event.Type == "error" || event.Type == "turn.failed" || event.Msg.Type == "error":
			return Launched{}, fmt.Errorf("codex ended the round with an error: %s", strings.TrimSpace(event.Message+event.Msg.Message))
		case event.Type == "item.completed" && event.Item.Type == "agent_message":
			launched.Message = event.Item.Text
		case event.Msg.Type == "agent_message":
			launched.Message = event.Msg.Message
		case event.ThreadID != "":
			launched.Session = event.ThreadID
		case event.SessionID != "":
			launched.Session = event.SessionID
		case event.Msg.SessionID != "":
			launched.Session = event.Msg.SessionID
		}
	}
	if launched.Session == "" {
		return Launched{}, errors.New("codex reported no session to resume")
	}
	return launched, nil
}

// DriverRequest is what a driver reads on stdin per round (V-D19).
type DriverRequest struct {
	Round       int    `json:"round"`
	Session     string `json:"session"`
	Instance    string `json:"instance"`
	Prompt      string `json:"prompt"`
	DriverState string `json:"driver_state"`
}

func runDriver(ctx context.Context, settings Settings, round Round) (string, error) {
	if settings.Driver.Command == "" {
		return "", errors.New("verify.runner driver needs verify.driver.command")
	}
	request, err := json.Marshal(DriverRequest{Round: round.Number, Session: round.Session, Instance: round.Instance, Prompt: round.Prompt, DriverState: round.Resume})
	if err != nil {
		return "", err
	}
	out, err := run(ctx, settings.WorkDir, settings.Driver.Command, settings.Driver.Args, string(request))
	if err != nil {
		return "", err
	}
	var answer struct {
		DriverState *string `json:"driver_state"`
	}
	err = json.Unmarshal(lastLine(out), &answer)
	if err != nil || answer.DriverState == nil {
		return "", fmt.Errorf("the driver printed no {\"driver_state\": ...} line: %q", strings.TrimSpace(string(out)))
	}
	return *answer.DriverState, nil
}

func run(ctx context.Context, dir, name string, args []string, stdin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	// A killed agent's own children may still hold its output open.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s did not end its round in time: %w", name, ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func lastLine(out []byte) []byte {
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	return lines[len(lines)-1]
}
