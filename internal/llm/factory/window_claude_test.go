package factory_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
)

// scriptedClaude puts a claude executable on PATH that appends its arguments
// to dir/calls, one call a line, and prints response.
func scriptedClaude(t *testing.T, response string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"$*\" >> '" + dir + "/calls'\ncat > /dev/null\necho '" + response + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func claudeCalls(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "calls"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

const probeAnswer = `{"type":"result","result":"OK","usage":{},"modelUsage":{"claude-haiku-4-5":{"inputTokens":1,"outputTokens":1},"claude-sonnet-4-6":{"inputTokens":9,"outputTokens":2,"contextWindow":1000000,"maxOutputTokens":128000}}}`

// claude-cli reports its window through one isolated probe call, asked once
// per process and model.
func TestWindowDetect_ClaudeCLIProbesModelUsage(t *testing.T) {
	dir := scriptedClaude(t, probeAnswer)
	detector := &factory.WindowDetector{}

	for range 2 {
		window, err := detector.Detect(t.Context(), model.LLMConfig{Provider: "claude-cli"})
		if err != nil {
			t.Fatal(err)
		}
		if window.Tokens != 1000000 || window.MaxOutput != 128000 {
			t.Fatalf("the window should come from the model's modelUsage, got %+v", window)
		}
	}

	calls := claudeCalls(t, dir)
	if len(calls) != 1 {
		t.Fatalf("the window should be probed once per process and model, probed %d times", len(calls))
	}
	for _, want := range []string{"-p", "--safe-mode", "--model " + model.DefaultLLMModel, "--output-format json"} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("the probe should run isolated on the resolved model, missing %q in %q", want, calls[0])
		}
	}
}

func TestWindowDetect_ClaudeCLIContextTokensWin(t *testing.T) {
	dir := scriptedClaude(t, probeAnswer)

	window, err := (&factory.WindowDetector{}).Detect(t.Context(), model.LLMConfig{Provider: "claude-cli", ContextTokens: 200000})
	if err != nil {
		t.Fatal(err)
	}
	if window.Tokens != 200000 || len(claudeCalls(t, dir)) != 0 {
		t.Fatalf("llm.context_tokens should win without a probe, got %+v after %d calls", window, len(claudeCalls(t, dir)))
	}
}

func TestWindowDetect_ClaudeCLIWithoutAWindowNamesTheKey(t *testing.T) {
	scriptedClaude(t, `{"type":"result","result":"OK","usage":{},"modelUsage":{"claude-sonnet-4-6":{"inputTokens":9}}}`)

	_, err := (&factory.WindowDetector{}).Detect(t.Context(), model.LLMConfig{Provider: "claude-cli"})
	if err == nil || !strings.Contains(err.Error(), "set llm.context_tokens") {
		t.Fatalf("a probe reporting no window should be refused naming the key, got %v", err)
	}
}
