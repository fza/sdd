package factory_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// fakeClaude puts a `claude` executable on PATH that runs the shell line then.
func fakeClaude(t *testing.T, then string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" + then + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The configured credentials setting reaches the runner; which variables it
// covers is the runner's own rule.
func TestClaudeCredentials(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setting model.ClaudeCLICredentials
		want    string
	}{
		{"unset strips the key", "", "absent"},
		{"login strips the key", model.ClaudeCLICredentialsLogin, "absent"},
		{"env passes the key through", model.ClaudeCLICredentialsEnv, "set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeClaude(t, `printf '{"result":"%s"}' "${ANTHROPIC_API_KEY-absent}"`)
			t.Setenv("ANTHROPIC_API_KEY", "set")

			runner, err := factory.New(model.LLMConfig{Provider: "claude-cli", ClaudeCLI: model.ClaudeCLIConfig{Credentials: tc.setting}})
			if err != nil {
				t.Fatal(err)
			}
			res, err := runner.Run(context.Background(), llm.Request{UserPrompt: "review"})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Text != tc.want {
				t.Errorf("the key should reach the child only with credentials env, got %q", res.Text)
			}
		})
	}
}

func TestClaudeRefusesUnknownCredentials(t *testing.T) {
	_, err := factory.New(model.LLMConfig{Provider: "claude-cli", ClaudeCLI: model.ClaudeCLIConfig{Credentials: "apikey"}})
	if err == nil || !strings.Contains(err.Error(), "login") || !strings.Contains(err.Error(), "env") {
		t.Errorf("an unknown credentials value should be refused, naming the allowed ones, got %v", err)
	}
}
