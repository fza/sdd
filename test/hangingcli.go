package test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// HangingCLI is an executable on PATH whose run never ends, as a CLI whose
// call hangs: it starts a child of its own holding its output, records its
// PID, then waits on the child. Recording after the fork keeps a group kill
// that follows Leader from missing a child still being forked.
type HangingCLI struct {
	dir string
}

// NewHangingCLI puts a hanging executable named name first on PATH for the
// rest of the test.
func NewHangingCLI(t *testing.T, name string) *HangingCLI {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in executable is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"sleep 300 &\n" +
		"echo $$ > '" + dir + "/leader.tmp'\n" +
		"mv '" + dir + "/leader.tmp' '" + dir + "/leader'\n" +
		"wait\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &HangingCLI{dir: dir}
}

// Leader waits for the first run to start and returns its PID, which leads
// the process group its runner starts it in; the group is killed when the
// test ends.
func (c *HangingCLI) Leader(t *testing.T) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(c.dir, "leader"))
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatal("the hanging CLI never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// GroupGone reports whether no process of the group group leads is left.
func GroupGone(group int) bool {
	return errors.Is(syscall.Kill(-group, 0), syscall.ESRCH)
}
