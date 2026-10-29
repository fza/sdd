package factory_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// outlastingAnswer stands in for a CLI whose answer outlasts the timeout.
const outlastingAnswer = "exec sleep 5"

func TestTimeoutNamesTheSetting(t *testing.T) {
	fakeClaude(t, outlastingAnswer)
	runner, err := factory.New(model.LLMConfig{Provider: "claude-cli", Timeout: "200ms"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), llm.Request{UserPrompt: "review"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a call outlasting its bound should fail as a deadline, got %v", err)
	}
	if !strings.Contains(err.Error(), "sdd config set llm.timeout") {
		t.Errorf("the timeout should name the setting that raises it, got %v", err)
	}
	var attributed *llm.Error
	if !errors.As(err, &attributed) {
		t.Errorf("the timeout should keep the provider's attribution, got %v", err)
	}
}

func TestCallerDeadlineIsNotBlamedOnTheSetting(t *testing.T) {
	fakeClaude(t, outlastingAnswer)
	runner, err := factory.New(model.LLMConfig{Provider: "claude-cli", Timeout: "1m"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = runner.Run(ctx, llm.Request{UserPrompt: "review"})
	if err == nil || strings.Contains(err.Error(), "llm.timeout") {
		t.Errorf("a deadline the caller set should not point at llm.timeout, got %v", err)
	}
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
	for _, want := range []string{"[--safe-mode]\n", "[--tools]\n[]\n", "[--strict-mcp-config]\n"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("claude -p should run in safe mode with no built-in and no MCP tools, got args %q", args)
		}
	}
}
