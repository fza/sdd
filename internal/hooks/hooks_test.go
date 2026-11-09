package hooks_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

func commandsIn(t *testing.T, dir string) *hooks.Commands {
	t.Helper()
	return hooks.New(func() ([]model.HookConfig, error) { return nil, nil }, dir)
}

func TestRunWritesThePayloadToStdinAndReturnsStdout(t *testing.T) {
	commands := commandsIn(t, t.TempDir())
	output, err := commands.Run(t.Context(), sdd.Hook{Command: "cat"}, []byte(`{"event":"entry.written"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"event":"entry.written"}` {
		t.Errorf("the hook should read the payload on stdin, wrote %q", output)
	}
}

func TestRunRunsInTheProjectDirectory(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	output, err := commandsIn(t, dir).Run(t.Context(), sdd.Hook{Command: "pwd -P"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != dir {
		t.Errorf("the hook should run in the project directory, ran in %q", output)
	}
}

func TestRunFailsOnANonzeroExitWithItsStderr(t *testing.T) {
	_, err := commandsIn(t, t.TempDir()).Run(t.Context(), sdd.Hook{Command: "echo lint broke >&2; exit 3"}, nil)
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "lint broke") {
		t.Fatalf("a nonzero exit should fail naming the status and the hook's stderr, got %v", err)
	}
}

func TestRunStopsTheHookAndItsChildrenWhenTheContextEnds(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := commandsIn(t, dir).Run(ctx, sdd.Hook{Command: "sleep 30 & echo $! > child.pid; wait"}, nil)
	if err == nil {
		t.Fatal("a hook outliving its context should fail")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the stopped hook should return promptly, took %s", elapsed)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("the hook's child should be stopped with it, still runs")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestHooksReadsTheConfigurationOnEveryCall(t *testing.T) {
	configs := []model.HookConfig{{Event: "entry.written", Command: "notify", Timeout: 5, Findings: true}}
	commands := hooks.New(func() ([]model.HookConfig, error) { return configs, nil }, t.TempDir())
	first, err := commands.Hooks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := sdd.Hook{Event: "entry.written", Command: "notify", Timeout: 5 * time.Second, Findings: true}
	if len(first) != 1 || first[0] != want {
		t.Fatalf("hooks = %+v, want the configured hook with its timeout in seconds", first)
	}
	configs = nil
	second, err := commands.Hooks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Errorf("a changed configuration should reach the next call, got %+v", second)
	}
}
