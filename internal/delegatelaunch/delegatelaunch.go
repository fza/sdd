// Package delegatelaunch runs the deep draft verifier's delegate without the
// drafting agent's harness: a headless claude run per round,
// resumed by the session id the first round reported, or an external driver
// per round. The agent reaches the child session only through an sdd MCP
// server of its own; findings and answers travel through that server, never
// through the launch.
package delegatelaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// RoundTimeout bounds one launched round where Settings.Timeout is unset.
const RoundTimeout = model.DefaultVerifierRoundTimeout

// Fallback asks for claude where Settings.Agents names it and its CLI is
// found.
const Fallback = "fallback"

// Settings is how one project launches its deep verifier.
type Settings struct {
	// Runner is claude, driver or Fallback.
	Runner string
	// Agents are the project's supported_agents, in order, for Fallback.
	Agents []string
	Driver model.VerifierDriver
	// Claude is the model and effort the headless claude runs with; empty
	// values leave the CLI's own.
	Claude model.VerifierAgent
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
	// Started, when set, is handed the PID of the agent CLI or driver once
	// it runs; an error stops it and fails the round.
	Started func(pid int) error
}

// Launched is what a round reports: the session to resume next round, the
// runner that ran, and the agent's last message, empty for a driver.
type Launched struct {
	Session string
	Runner  string
	Message string
}

// claudeTools are the tools a headless claude verifier may use: read,
// search and glob, the web and the sdd MCP tools, read_git among them for
// history (20261123-114150-d-tac-d8f).
var claudeTools = []string{
	"Read", "Grep", "Glob", "WebFetch", "WebSearch", "mcp__sdd__*",
}

// Resolve names the runner Run runs: settings.Runner, or for Fallback claude
// where settings.Agents names it and its CLI is found.
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
		if agent != string(model.VerifierRunnerClaude) {
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

// ClaudeArgs are the flags of a headless claude round: print mode
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
		"--tools", "Read,Grep,Glob,WebFetch,WebSearch",
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
	out, err := run(ctx, settings.WorkDir, "claude", args, round.Prompt, round.Started)
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

// DriverRequest is what a driver reads on stdin per round.
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
	out, err := run(ctx, settings.WorkDir, settings.Driver.Command, settings.Driver.Args, string(request), round.Started)
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

func run(ctx context.Context, dir string, name string, args []string, stdin string, started func(pid int) error) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	// A killed agent's own children may still hold its output open.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Start()
	if err == nil && started != nil {
		if startedErr := started(cmd.Process.Pid); startedErr != nil {
			killErr := cmd.Process.Kill()
			if errors.Is(killErr, os.ErrProcessDone) {
				killErr = nil
			}
			// Reaps the process killed above; its error is that kill.
			_ = cmd.Wait()
			return nil, errors.Join(fmt.Errorf("recording the %s process: %w", name, startedErr), killErr)
		}
	}
	if err == nil {
		err = cmd.Wait()
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s did not end its round in time: %w", name, ctx.Err())
	}
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func lastLine(out []byte) []byte {
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	return lines[len(lines)-1]
}
