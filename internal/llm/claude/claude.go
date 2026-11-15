// Package claude implements llm.Runner by invoking the Claude CLI.
package claude

import (
	"context"
	"encoding/json"
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

// scrubbedEnvVars name the credentials that redirect the CLI away from the
// signed-in account. This transport exists to answer on that account, so an
// inherited key would silently move the bill to an API balance with nothing in
// the output saying which credential answered. A setup authenticating the CLI
// by key says so with llm.claude_cli.credentials: env.
var scrubbedEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
}

// Run executes claude -p --output-format json and parses the JSON response.
// The claude CLI accepts a single stdin payload, so SystemPrompt and
// UserPrompt are concatenated — prompt caching is not available through this
// transport. A ResponseSchema goes to --json-schema, and the structured output
// the CLI validated against it becomes the text.
//
// The process runs isolated: --safe-mode, no tools, a working directory of its
// own, and by default no Anthropic credentials in its environment. Started inside a
// project tree the CLI loads that project's instruction files, skills, hooks
// and configured servers, and every one of them counts against a prompt that
// needs none of them. The calls are text reviews: `--tools ""` removes the
// built-in tools and `--strict-mcp-config` the configured MCP servers, whose
// tool definitions would otherwise load in full into every prompt. A draft's
// text then has nothing to act through. `--no-session-persistence` keeps the
// CLI from saving each call's transcript, the full draft, under ~/.claude.
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
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	if r.credentials != model.ClaudeCLICredentialsEnv {
		cmd.Env = scrubbedEnv(cmd.Env)
	}
	cmd.Stdin = strings.NewReader(req.Combined())
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("claude -p timed out (increase with --preflight-timeout)")
		} else {
			err = fmt.Errorf("claude -p: %w", err)
		}
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: err}
	}

	var resp claudeResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: fmt.Errorf("parsing claude JSON response: %w", err)}
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

// scrubbedEnv returns env without the credential variables, matched by name so
// a variable whose value merely looks like a key is left alone.
func scrubbedEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if slices.Contains(scrubbedEnvVars, name) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// claudeResponse maps the JSON output of claude -p --output-format json.
type claudeResponse struct {
	Result           string                      `json:"result"`
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
