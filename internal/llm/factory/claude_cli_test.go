package factory_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// fakeClaude puts a `claude` executable on PATH that records its arguments
// before running the shell line then, so a test sees what the runner passed.
func fakeClaude(t *testing.T, then string) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nfor arg in \"$@\"; do printf '[%s]\\n' \"$arg\"; done > " + argsFile + "\n" + then + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

func TestClaudeRunsWithoutTools(t *testing.T) {
	argsFile := fakeClaude(t, `printf '{"result":"ok"}'`)
	runner, err := factory.New(model.LLMConfig{Provider: "claude-cli"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), llm.Request{UserPrompt: "review"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[--tools]\n[]\n", "[--strict-mcp-config]\n"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("claude -p should run with no built-in and no MCP tools, got args %q", args)
		}
	}
}
