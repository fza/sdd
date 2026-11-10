// Package llmdebug writes a transcript of every LLM call while llm.debug is on,
// so a checker's verdict can be traced to the prompt that produced it
// (20261110-150803-s-tac-9u1).
package llmdebug

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/networkteam/sdd/pkg/llm"
)

// Dir is the transcript directory below a project's .sdd/ directory.
const Dir = "tmp/llm-debug"

// fence sets off prompts and responses, which may hold markdown code fences
// of their own.
const fence = "~~~~~~~~"

var sequence atomic.Uint64

// Transcribed decorates r so every call, success or failure, writes one
// transcript file into dir. A transcript that cannot be written fails the
// call: the switch was turned on to get one.
func Transcribed(r llm.Runner, dir string) llm.Runner {
	return llm.RunnerFunc(func(ctx context.Context, req llm.Request) (llm.Result, error) {
		start := time.Now()
		result, err := r.Run(ctx, req)
		writeErr := write(dir, start, time.Since(start), req, result, err)
		if writeErr != nil {
			return result, errors.Join(err, fmt.Errorf("writing the llm.debug transcript: %w", writeErr))
		}
		return result, err
	})
}

func write(dir string, start time.Time, took time.Duration, req llm.Request, result llm.Result, callErr error) error {
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s-%d.md", start.UTC().Format("20060102T150405.000000000Z"), req.Purpose, sequence.Add(1))
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(Render(start, took, req, result, callErr))
	return errors.Join(err, file.Close())
}

// Render is one call's transcript: what served it, how long it took and what
// it cost, then both prompt parts, the response schema and the raw response.
func Render(start time.Time, took time.Duration, req llm.Request, result llm.Result, callErr error) string {
	identity, usage, finish := result.Identity, result.Usage, result.FinishReason
	if attributed, ok := errors.AsType[*llm.Error](callErr); ok {
		identity, usage, finish = attributed.Identity, attributed.Usage, attributed.FinishReason
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", req.Purpose)
	fmt.Fprintf(&b, "- started: %s\n", start.UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "- duration_ms: %d\n", took.Milliseconds())
	fmt.Fprintf(&b, "- provider: %s\n- model: %s\n", identity.Provider, identity.Model)
	if identity.Variant != "" {
		fmt.Fprintf(&b, "- variant: %s\n", identity.Variant)
	}
	fmt.Fprintf(&b, "- input_tokens: %d\n- output_tokens: %d\n- reasoning_tokens: %d\n", usage.InputTokens, usage.OutputTokens, usage.ReasoningTokens)
	if finish != "" {
		fmt.Fprintf(&b, "- finish_reason: %s\n", finish)
	}
	if callErr != nil {
		fmt.Fprintf(&b, "- error: %s\n", callErr)
	}
	section := func(title, text string) {
		fmt.Fprintf(&b, "\n## %s\n\n%s\n%s\n%s\n", title, fence, text, fence)
	}
	section("System prompt", req.SystemPrompt)
	section("User prompt", req.UserPrompt)
	if len(req.ResponseSchema) > 0 {
		section("Response schema", string(req.ResponseSchema))
	}
	section("Response", result.Text)
	return b.String()
}
