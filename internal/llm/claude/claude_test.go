package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/claude"
	"github.com/networkteam/sdd/pkg/llm"
)

// What the child process actually sees is the whole point of the isolation, and
// it is not inferable from the adapter's own state. A stand-in binary on PATH
// reports its arguments, working directory and credential environment back
// through the response the runner parses.
func TestChildRunsIsolated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}

	binDir := t.TempDir()
	script := filepath.Join(binDir, "claude")
	body := `#!/bin/sh
printf '{"result":"args=%s cwd=%s key=%s token=%s base=%s","usage":{"input_tokens":1,"output_tokens":2}}\n' \
  "$*" "$PWD" "${ANTHROPIC_API_KEY:-absent}" "${ANTHROPIC_AUTH_TOKEN:-absent}" "${ANTHROPIC_BASE_URL:-absent}"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("writing stand-in binary: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-reach-the-child")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "should-not-reach-the-child")
	t.Setenv("ANTHROPIC_BASE_URL", "https://should-not-reach-the-child")

	invocationDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	res, err := claude.NewRunner("claude-sonnet-4-6").Run(context.Background(), llm.Request{UserPrompt: "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(res.Text, "--safe-mode") {
		t.Errorf("the child must run in safe mode, got %q", res.Text)
	}
	for _, leaked := range []string{"key=sk-should-not-reach-the-child", "token=should-not-reach-the-child", "base=https://should-not-reach-the-child"} {
		if strings.Contains(res.Text, leaked) {
			t.Errorf("credential reached the child: %q", res.Text)
		}
	}
	if !strings.Contains(res.Text, "key=absent") {
		t.Errorf("ANTHROPIC_API_KEY must be absent, not empty or set: %q", res.Text)
	}

	childDir := valueOf(res.Text, "cwd=")
	if childDir == "" {
		t.Fatalf("no working directory reported: %q", res.Text)
	}
	if sameDir(t, childDir, invocationDir) {
		t.Errorf("the child ran in the invocation's directory (%s), loading the project's own context", childDir)
	}
	if _, err := os.Stat(childDir); !os.IsNotExist(err) {
		t.Errorf("working directory %s outlived the call (stat err %v)", childDir, err)
	}
}

func valueOf(text, key string) string {
	_, rest, found := strings.Cut(text, key)
	if !found {
		return ""
	}
	value, _, _ := strings.Cut(rest, " ")
	return value
}

// A temporary directory resolves through symlinks on macOS, so the comparison
// is made on resolved paths.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}
