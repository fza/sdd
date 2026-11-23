// Package claude implements llm.Runner by invoking the Claude CLI.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// Runner invokes the claude CLI with --output-format json and maps the
// response into the common llm.Result.
type Runner struct {
	model       string
	effort      string
	credentials model.ClaudeCLICredentials
}

// NewRunner returns an llm.Runner backed by the claude CLI, passing effort to
// --effort as written; an empty effort leaves the CLI's own. Only
// model.ClaudeCLICredentialsEnv keeps the Anthropic credentials in the child's
// environment.
func NewRunner(modelName, effort string, credentials model.ClaudeCLICredentials) *Runner {
	return &Runner{model: modelName, effort: effort, credentials: credentials}
}

func (r *Runner) identity() llm.Identity {
	return llm.Identity{Provider: "claude-cli", Model: r.model}
}

// loginKeptEnvVars are the only variables of Claude's own name space that
// login mode passes: the login's, and TLS and path settings. Every other
// ANTHROPIC_* or CLAUDE_* variable may move the call off the signed-in
// account, and each one the reviews found sat in that name space; provider
// credentials outside it, such as AWS_*, take effect only through a
// CLAUDE_CODE_USE_* switch inside it.
var loginKeptEnvVars = []string{
	"CLAUDE_CONFIG_DIR", "ANTHROPIC_CONFIG_DIR", "CLAUDE_SECURESTORAGE_CONFIG_DIR",
	"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR",
	"CLAUDE_CODE_CERT_STORE", "CLAUDE_CODE_CLIENT_CERT", "CLAUDE_CODE_CLIENT_KEY", "CLAUDE_CODE_CLIENT_KEY_PASSPHRASE",
	"CLAUDE_CODE_DISABLE_LOOPED_CROSS_CERT_DROP", "CLAUDE_CODE_DISABLE_SELF_NAMED_CERT_DROP",
	"CLAUDE_CODE_GIT_BASH_PATH", "CLAUDE_CODE_TMPDIR",
}

// parentSessionEnvVars are what a Claude Code session hands its child
// processes, sdd's server among them. Inherited, they would make the call
// count itself as a nested session of that one, take over its session ID and
// launch, and reach its messaging inbox; the call belongs to sdd, so every mode
// drops them.
var parentSessionEnvVars = []string{
	"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_PID", "CLAUDE_EFFORT", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH",
}

const waitDelay = 2 * time.Second

// Run executes claude -p --output-format json and parses the JSON response.
// The claude CLI accepts a single stdin payload, so SystemPrompt and
// UserPrompt are concatenated — prompt caching is not available through this
// transport. A ResponseSchema goes to --json-schema, and the structured output
// the CLI validated against it becomes the text.
//
// Every call is a text review, so the process runs without tools, project
// context, customizations or a saved session, each of which would cost prompt
// tokens or keep the draft; the Claude Code CLI section of the configuration
// reference names each flag.
func (r *Runner) Run(ctx context.Context, req llm.Request) (llm.Result, error) {
	workDir, err := os.MkdirTemp("", "sdd-claude-")
	if err != nil {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: fmt.Errorf("creating working directory: %w", err)}
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	args := []string{"-p", "--safe-mode", "--no-session-persistence", "--tools", "", "--strict-mcp-config", "--model", r.model, "--output-format", "json"}
	if r.effort != "" {
		args = append(args, "--effort", r.effort)
	}
	if len(req.ResponseSchema) > 0 {
		args = append(args, "--json-schema", string(req.ResponseSchema))
	}
	env := withoutVars(os.Environ(), func(name string) bool { return slices.Contains(parentSessionEnvVars, name) })
	if r.credentials != model.ClaudeCLICredentialsEnv {
		env = withoutVars(env, loginDrops)
		// A settings file's env block and apiKeyHelper apply even under --safe-mode.
		args = append(args, "--setting-sources", "")
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	// The CLI's own children may hold stdout open: a cancel kills its
	// whole process group, and Wait gives up on the pipes after waitDelay.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	cmd.Dir = workDir
	// Go sets PWD itself only for a nil Env.
	cmd.Env = append(env, "PWD="+workDir)
	cmd.Stdin = strings.NewReader(req.Combined())
	out, runErr := cmd.Output()
	if runErr != nil && ctx.Err() != nil {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: fmt.Errorf("claude -p: %w", ctx.Err())}
	}

	var resp claudeResponse
	parseErr := json.Unmarshal(out, &resp)
	if runErr != nil || resp.IsError {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: r.failure(runErr, resp)}
	}
	if parseErr != nil {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: fmt.Errorf("parsing claude JSON response: %w", parseErr)}
	}
	text := resp.Result
	if len(req.ResponseSchema) > 0 {
		if len(resp.StructuredOutput) == 0 || string(resp.StructuredOutput) == "null" {
			return llm.Result{}, &llm.Error{Identity: r.identity(), Err: fmt.Errorf("claude -p returned no structured output for the response schema")}
		}
		text = string(resp.StructuredOutput)
	}

	// The CLI is the one transport reporting per-model usage (a call may span
	// e.g. a haiku sub-model); the common Usage carries the aggregate, so the
	// per-model detail is this runner's own debug logging.
	if len(resp.ModelUsage) > 0 {
		attrs := make([]any, 0, len(resp.ModelUsage))
		for name, mu := range resp.ModelUsage {
			attrs = append(attrs, slog.Group(name,
				slog.Int("tokens.in", mu.InputTokens),
				slog.Int("tokens.out", mu.OutputTokens),
				slog.Int("cache.read", mu.CacheReadInputTokens),
				slog.Int("cache.create", mu.CacheCreationInputTokens),
				slog.Float64("cost", mu.CostUSD),
			))
		}
		slogutils.FromContext(ctx).Debug("claude-cli model usage", slog.Group("llm", attrs...))
	}

	return llm.Result{
		Text:     text,
		Identity: r.identity(),
		Usage: llm.Usage{
			InputTokens:       resp.Usage.InputTokens,
			OutputTokens:      resp.Usage.OutputTokens,
			CacheReadTokens:   resp.Usage.CacheReadInputTokens,
			CacheCreateTokens: resp.Usage.CacheCreationInputTokens,
			CostUSD:           resp.TotalCostUSD,
		},
	}, nil
}

var errReportedError = errors.New("reported an error")

// loginFailureMarkers are what the CLI reports when no credential it can use
// answers: no login, or a rejected one.
var loginFailureMarkers = []string{"/login", "authentication_error"}

// failure describes a failed call by what the CLI reported: the result or
// errors of an is_error response, its subtype when it says nothing else, and
// its stderr. In login mode a login failure names the ways out, since a key
// exported for the CLI no longer reaches it.
func (r *Runner) failure(runErr error, resp claudeResponse) error {
	var reported []string
	if resp.IsError {
		if resp.Result != "" {
			reported = append(reported, resp.Result)
		}
		reported = append(reported, resp.Errors...)
		if len(reported) == 0 && resp.Subtype != "" {
			reported = append(reported, resp.Subtype)
		}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			reported = append(reported, stderr)
		}
	}
	cause := runErr
	if cause == nil {
		cause = errReportedError
	}
	if len(reported) == 0 {
		return fmt.Errorf("claude -p: %w", cause)
	}
	detail := strings.Join(reported, "; ")
	err := fmt.Errorf("claude -p: %w: %s", cause, detail)
	if r.credentials != model.ClaudeCLICredentialsEnv && slices.ContainsFunc(loginFailureMarkers, func(marker string) bool { return strings.Contains(detail, marker) }) {
		return fmt.Errorf("%w (sign in to Claude Code with /login, or set llm.claude_cli.credentials: env to authenticate from the environment)", err)
	}
	return err
}

// withoutVars returns env without the variables drop names, matched by name so
// a variable whose value merely looks like a key is left alone, and without
// regard to case, as Windows reads the names. drop sees the name upper-cased.
func withoutVars(env []string, drop func(name string) bool) []string {
	return slices.DeleteFunc(env, func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return drop(strings.ToUpper(name))
	})
}

func loginDrops(name string) bool {
	claudes := strings.HasPrefix(name, "ANTHROPIC_") || strings.HasPrefix(name, "CLAUDE_")
	return claudes && !slices.Contains(loginKeptEnvVars, name)
}

// claudeResponse maps the JSON output of claude -p --output-format json.
type claudeResponse struct {
	Result           string                      `json:"result"`
	Subtype          string                      `json:"subtype"`
	Errors           []string                    `json:"errors"`
	StructuredOutput json.RawMessage             `json:"structured_output"`
	TotalCostUSD     float64                     `json:"total_cost_usd"`
	DurationMs       int64                       `json:"duration_ms"`
	DurationAPIMs    int64                       `json:"duration_api_ms"`
	NumTurns         int                         `json:"num_turns"`
	IsError          bool                        `json:"is_error"`
	Usage            claudeUsage                 `json:"usage"`
	ModelUsage       map[string]claudeModelUsage `json:"modelUsage"`
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type claudeModelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
}
