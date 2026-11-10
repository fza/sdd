package llmdebug_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmdebug"
	"github.com/networkteam/sdd/pkg/llm"
)

func TestTranscribedWritesOneTranscriptPerCall(t *testing.T) {
	cases := []struct {
		name   string
		result llm.Result
		err    error
		want   []string
	}{
		{
			name:   "success",
			result: llm.Result{Text: `{"findings":[]}`, Identity: llm.Identity{Provider: "openai", Model: "small", Variant: "reasoning_effort=low"}, Usage: llm.Usage{InputTokens: 120, OutputTokens: 7}, FinishReason: "stop"},
			want:   []string{"# preflight", "- provider: openai", "- model: small", "- variant: reasoning_effort=low", "- input_tokens: 120", "- output_tokens: 7", "- finish_reason: stop", "## System prompt", "the rules", "## User prompt", "the draft", "## Response schema", `{"type":"object"}`, "## Response", `{"findings":[]}`},
		},
		{
			name: "failure",
			err:  &llm.Error{Identity: llm.Identity{Provider: "openai", Model: "small"}, Err: errors.New("endpoint refused")},
			want: []string{"- provider: openai", "- error: ", "endpoint refused", "## User prompt", "the draft"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "llm-debug")
			inner := llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) { return tc.result, tc.err })
			runner := llmdebug.Transcribed(inner, dir)
			req := llm.Request{Purpose: llm.PurposePreflight, SystemPrompt: "the rules", UserPrompt: "the draft", ResponseSchema: []byte(`{"type":"object"}`)}

			result, err := runner.Run(context.Background(), req)
			if !errors.Is(err, tc.err) || result.Text != tc.result.Text {
				t.Fatalf("the call's own result and error should pass through, got %q and %v", result.Text, err)
			}
			files, readErr := os.ReadDir(dir)
			if readErr != nil || len(files) != 1 {
				t.Fatalf("one call should write one transcript, got %v (%v)", files, readErr)
			}
			if name := files[0].Name(); !strings.Contains(name, "-preflight-") || !strings.HasSuffix(name, ".md") {
				t.Errorf("the transcript name should carry the call's purpose, got %q", files[0].Name())
			}
			data, readErr := os.ReadFile(filepath.Join(dir, files[0].Name()))
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(data), want) {
					t.Errorf("the transcript should carry %q:\n%s", want, data)
				}
			}
		})
	}
}

func TestTranscribedFailsTheCallWhenNoTranscriptCanBeWritten(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	err := os.WriteFile(blocked, nil, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	inner := llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) { return llm.Result{Text: "ok"}, nil })
	_, err = llmdebug.Transcribed(inner, filepath.Join(blocked, "llm-debug")).Run(context.Background(), llm.Request{Purpose: llm.PurposeSummarize})
	if err == nil || !strings.Contains(err.Error(), "llm.debug transcript") {
		t.Fatalf("a transcript that cannot be written should fail the call, got %v", err)
	}
}
