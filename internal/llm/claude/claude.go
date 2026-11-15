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

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// Runner invokes the claude CLI with --output-format json and maps the
// response into the common llm.Result.
type Runner struct {
	model       string
	credentials model.ClaudeCLICredentials
}

// NewRunner returns an llm.Runner backed by the claude CLI. Only
// model.ClaudeCLICredentialsEnv keeps the Anthropic credentials in the child's
// environment.
func NewRunner(modelName string, credentials model.ClaudeCLICredentials) *Runner {
	return &Runner{model: modelName, credentials: credentials}
}

func (r *Runner) identity() llm.Identity {
	return llm.Identity{Provider: "claude-cli", Model: r.model}
}

// scrubbedEnvPrefixes name the variables that redirect the CLI away from the
// signed-in account: keys, tokens, endpoints, headers and provider switches.
// This transport exists to answer on that account, so an inherited one would
// silently move the bill with nothing in the output saying which credential
// answered. Matching the families, not a list, keeps up with providers the CLI
// adds. A setup authenticating the CLI otherwise says so with
// llm.claude_cli.credentials: env.
var scrubbedEnvPrefixes = []string{"ANTHROPIC_", "CLAUDE_CODE_USE_"}

// scrubbedEnvVars hand the CLI a key or gateway token outside both families.
var scrubbedEnvVars = []string{"CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR", "CLAUDE_CODE_GATEWAY_TOKEN_FILE_DESCRIPTOR"}

// loginEnvVars pick which signed-in profile answers, so they stay.
var loginEnvVars = []string{"ANTHROPIC_PROFILE", "ANTHROPIC_CONFIG_DIR"}

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
	if len(req.ResponseSchema) > 0 {
		args = append(args, "--json-schema", string(req.ResponseSchema))
	}
	env := os.Environ()
	if r.credentials != model.ClaudeCLICredentialsEnv {
		env = scrubbedEnv(env)
		// A settings file's env block and apiKeyHelper apply even under --safe-mode.
		args = append(args, "--setting-sources", "")
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(req.Combined())
	out, runErr := cmd.Output()
	if runErr != nil && ctx.Err() == context.DeadlineExceeded {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: fmt.Errorf("claude -p timed out (increase with --preflight-timeout)")}
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

// scrubbedEnv returns env without the credential variables, matched by name so
// a variable whose value merely looks like a key is left alone.
func scrubbedEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if scrubbed(name) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func scrubbed(name string) bool {
	if slices.Contains(loginEnvVars, name) {
		return false
	}
	return slices.Contains(scrubbedEnvVars, name) ||
		slices.ContainsFunc(scrubbedEnvPrefixes, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
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
