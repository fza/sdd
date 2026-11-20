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

// awaitGrandchild waits for the PID the stand-in CLI of hangingCLI records.
func awaitGrandchild(t *testing.T, dir string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if raw, err := os.ReadFile(filepath.Join(dir, "grandchild")); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
				t.Cleanup(func() {
					if alive(pid) {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				})
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the stand-in CLI never started its child")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Killing a process's children ends a call whose context never ends with its
// whole process group, and no call starts after it.
func TestKillEndsTheCallsOfItsChildren(t *testing.T) {
	dir := hangingCLI(t)
	children := claude.NewChildren()
	ctx := claude.WithChildren(t.Context(), children)
	runner := claude.NewRunner("sonnet", "", model.ClaudeCLICredentialsLogin)
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, llm.Request{UserPrompt: "U"})
		done <- err
	}()
	grandchild := awaitGrandchild(t, dir)

	killCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := children.Kill(killCtx); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a killed call should fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a killed call should return")
	}
	deadline := time.Now().Add(2 * time.Second)
	for alive(grandchild) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d of the killed call's group outlived Kill", grandchild)
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := os.Remove(filepath.Join(dir, "grandchild")); err != nil {
		t.Fatal(err)
	}
	_, err := runner.Run(ctx, llm.Request{UserPrompt: "U"})
	if !errors.Is(err, claude.ErrChildrenKilled) {
		t.Fatalf("Run after Kill = %v: a call should be refused once the children were killed", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "grandchild")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused call should not start the CLI")
	}
}
