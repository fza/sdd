// Package codex implements llm.Runner by invoking `codex exec`
// (20261109-222220-d-tac-4ev).
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/networkteam/sdd/pkg/llm"
)

// provider is the name this runner reports in its identity.
const provider = "codex"

// Runner invokes codex exec with JSON events and maps the final agent message
// and the turn's usage into the common llm.Result.
type Runner struct {
	model  string
	effort string
}

// NewRunner returns an llm.Runner backed by codex exec. model goes to -m and
// effort to model_reasoning_effort, both as written; an empty value leaves
// codex's own default.
func NewRunner(model, effort string) *Runner {
	return &Runner{model: model, effort: effort}
}

func (r *Runner) identity() llm.Identity {
	return llm.Identity{Provider: provider, Model: r.model}
}

// execArgs are the flags of one call: exec with JSON events in a read-only
// sandbox outside any repository, persisting no session, without the user's
// config.toml, execpolicy rules or project instructions, and the prompt read
// from stdin. schemaFile, when set, is the --output-schema the final message
// must match.
func execArgs(model, effort, schemaFile string) []string {
	args := []string{
		"exec", "--json", "--sandbox", "read-only", "--skip-git-repo-check", "--ephemeral",
		"--ignore-user-config", "--ignore-rules", "-c", "project_doc_max_bytes=0",
	}
	if model != "" {
		args = append(args, "-m", model)
	}
	if effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(effort))
	}
	if schemaFile != "" {
		args = append(args, "--output-schema", schemaFile)
	}
	return append(args, "-")
}

// Run executes codex exec in a working directory of its own, so no project's
// instructions or files reach a prompt that needs none of them. SystemPrompt
// and UserPrompt are concatenated onto stdin. A ResponseSchema goes to
// --output-schema, and the final message must be JSON.
func (r *Runner) Run(ctx context.Context, req llm.Request) (llm.Result, error) {
	workDir, err := os.MkdirTemp("", "sdd-codex-")
	if err != nil {
		return llm.Result{}, r.fail(fmt.Errorf("creating working directory: %w", err))
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	schemaFile := ""
	if len(req.ResponseSchema) > 0 {
		schemaFile = filepath.Join(workDir, "schema.json")
		if err := os.WriteFile(schemaFile, req.ResponseSchema, 0o600); err != nil {
			return llm.Result{}, r.fail(fmt.Errorf("writing the response schema: %w", err))
		}
	}
	cmd := exec.CommandContext(ctx, "codex", execArgs(r.model, r.effort, schemaFile)...)
	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(req.Combined())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return llm.Result{}, r.fail(fmt.Errorf("codex exec: %w", ctx.Err()))
		}
		return llm.Result{}, r.fail(fmt.Errorf("codex exec: %w: %s", err, strings.TrimSpace(stderr.String())))
	}

	text, usage, err := parseEvents(out)
	if err != nil {
		return llm.Result{}, r.fail(err)
	}
	if len(req.ResponseSchema) > 0 && !json.Valid([]byte(text)) {
		return llm.Result{}, r.fail(fmt.Errorf("codex exec answered no JSON for the response schema: %q", text))
	}
	return llm.Result{Text: text, Identity: r.identity(), Usage: usage}, nil
}

func (r *Runner) fail(err error) error {
	return &llm.Error{Identity: r.identity(), Err: err}
}

// event is one JSONL line of codex exec --json.
type event struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Item    struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Usage struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
		OutputTokens      int `json:"output_tokens"`
	} `json:"usage"`
}

// parseEvents returns the last agent message and the turns' summed usage,
// failing on an error event or on a run that delivered no message.
func parseEvents(out []byte) (string, llm.Usage, error) {
	var text string
	var answered bool
	var usage llm.Usage
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("{")) {
			continue
		}
		var e event
		if err := json.Unmarshal(line, &e); err != nil {
			return "", llm.Usage{}, fmt.Errorf("parsing codex exec event %q: %w", line, err)
		}
		switch e.Type {
		case "error":
			return "", llm.Usage{}, fmt.Errorf("codex exec failed: %s", e.Message)
		case "turn.failed":
			return "", llm.Usage{}, fmt.Errorf("codex exec failed: %s", e.Error.Message)
		case "item.completed":
			if e.Item.Type == "agent_message" {
				text, answered = e.Item.Text, true
			}
		case "turn.completed":
			usage.InputTokens += e.Usage.InputTokens
			usage.CacheReadTokens += e.Usage.CachedInputTokens
			usage.OutputTokens += e.Usage.OutputTokens
		}
	}
	if !answered {
		return "", llm.Usage{}, errors.New("codex exec delivered no agent message")
	}
	return text, usage, nil
}
