package factory_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// fakeCLIs puts claude and codex executables on PATH that record their
// arguments into dir and answer with a minimal successful response.
func fakeCLIs(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binaries are shell scripts")
	}
	dir := t.TempDir()
	answers := map[string]string{
		"claude": `{"type":"result","result":"OK","usage":{}}`,
		"codex":  `{"type":"item.completed","item":{"type":"agent_message","text":"OK"}}`,
	}
	for name, answer := range answers {
		script := "#!/bin/sh\necho \"$*\" > '" + dir + "/" + name + ".args'\ncat > /dev/null\necho '" + answer + "'\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func runOnce(t *testing.T, cfg model.LLMConfig) llm.Result {
	t.Helper()
	runner, err := factory.New(cfg)
	if err != nil {
		t.Fatalf("New(%+v): %v", cfg, err)
	}
	res, err := runner.Run(t.Context(), llm.Request{Purpose: llm.PurposeSummarize, UserPrompt: "U"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func readArgs(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name+".args"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

// llm.provider codex runs the call through codex exec; llm.effort reaches
// codex's model_reasoning_effort and claude's --effort, medium when unset.
func TestNew_CLIProvidersTakeTheEffort(t *testing.T) {
	dir := fakeCLIs(t)

	res := runOnce(t, model.LLMConfig{Provider: "codex"})
	args := readArgs(t, dir, "codex")
	if res.Identity.Provider != "codex" || !strings.HasPrefix(args, "exec --json") || !strings.Contains(args, `model_reasoning_effort="medium"`) || strings.Contains(args, " -m ") {
		t.Errorf("codex should run codex exec at the default effort on its own model, got identity %+v and args %q", res.Identity, args)
	}
	runOnce(t, model.LLMConfig{Provider: "codex", Model: "gpt-5.5", Effort: "high"})
	if args := readArgs(t, dir, "codex"); !strings.Contains(args, "-m gpt-5.5") || !strings.Contains(args, `model_reasoning_effort="high"`) {
		t.Errorf("codex should take the model and effort as written, got %q", args)
	}

	runOnce(t, model.LLMConfig{})
	if args := readArgs(t, dir, "claude"); !strings.Contains(args, "--effort medium") || !strings.Contains(args, "--model "+model.DefaultLLMModel) {
		t.Errorf("claude-cli should run the default model at the default effort, got %q", args)
	}
	runOnce(t, model.LLMConfig{Provider: "claude-cli", Effort: "max"})
	if args := readArgs(t, dir, "claude"); !strings.Contains(args, "--effort max") {
		t.Errorf("claude-cli should take the effort as written, got %q", args)
	}
}
