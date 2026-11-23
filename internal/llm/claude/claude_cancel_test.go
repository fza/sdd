package claude_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/llm/claude"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// hangingCLI puts a claude executable on PATH that starts a child of its
// own holding stdout, records that child's PID and waits for it, as a CLI
// whose call never ends would.
func hangingCLI(t *testing.T) (dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	dir = t.TempDir()
	script := "#!/bin/sh\n" +
		"sleep 300 &\n" +
		"echo $! > '" + dir + "/grandchild.tmp'\n" +
		"mv '" + dir + "/grandchild.tmp' '" + dir + "/grandchild'\n" +
		"wait\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// A cancelled call ends at once and leaves no process behind: the CLI and
// every process it started are killed with its process group, though one of
// them holds the output pipe open.
func TestRunKillsTheCLIAndItsChildrenWhenCancelled(t *testing.T) {
	dir := hangingCLI(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := claude.NewRunner("sonnet", "", model.ClaudeCLICredentialsLogin).Run(ctx, llm.Request{UserPrompt: "U"})
		done <- err
	}()
	var grandchild int
	deadline := time.Now().Add(10 * time.Second)
	for grandchild == 0 {
		if raw, err := os.ReadFile(filepath.Join(dir, "grandchild")); err == nil {
			grandchild, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		}
		if time.Now().After(deadline) {
			t.Fatal("the stand-in CLI never started its child")
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Cleanup(func() {
		if alive(grandchild) {
			_ = syscall.Kill(grandchild, syscall.SIGKILL)
		}
	})
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run = %v: a cancelled call should end with its cause", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of its cancel: a child holding stdout outlived the CLI")
	}
	deadline = time.Now().Add(2 * time.Second)
	for alive(grandchild) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d the CLI started outlived the cancelled call", grandchild)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
