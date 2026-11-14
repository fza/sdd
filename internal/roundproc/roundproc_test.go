package roundproc_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/roundproc"
)

// eventually waits up to five seconds for done.
func eventually(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen in time", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A started process leads a process group of its own: it runs until stopped,
// and stopping it ends the processes it started too.
func TestStopEndsTheProcessGroup(t *testing.T) {
	dir := t.TempDir()
	process, err := roundproc.Start(dir, "/bin/sh", "-c", "sleep 30 & echo $! > child; wait")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-process.PID, syscall.SIGKILL) })
	var child int
	eventually(t, "the process starting its child", func() bool {
		out, err := os.ReadFile(filepath.Join(dir, "child"))
		child, _ = strconv.Atoi(strings.TrimSpace(string(out)))
		return err == nil && child > 0
	})
	if group, err := syscall.Getpgid(process.PID); err != nil || group != process.PID {
		t.Fatalf("the process should lead its own group, got group %d, %v", group, err)
	}
	if alive, err := roundproc.Alive(process); err != nil || !alive {
		t.Fatalf("the started process should run, got %v, %v", alive, err)
	}
	if alive, err := roundproc.Alive(roundproc.Process{PID: process.PID, Started: "another start"}); err != nil || alive {
		t.Fatalf("a process with the PID that started at another time is another one, got %v, %v", alive, err)
	}

	if err := roundproc.Stop(process); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the process ending", func() bool {
		alive, err := roundproc.Alive(process)
		return err == nil && !alive
	})
	eventually(t, "the process's child ending", func() bool {
		return errors.Is(syscall.Kill(child, 0), syscall.ESRCH)
	})
	if err := roundproc.Stop(process); err != nil {
		t.Fatalf("stopping a process already gone should leave it alone, got %v", err)
	}
}

// A process that ended on its own is gone.
func TestAProcessThatEndedIsGone(t *testing.T) {
	process, err := roundproc.Start(t.TempDir(), "/bin/sh", "-c", "sleep 0.2")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the process ending", func() bool {
		alive, err := roundproc.Alive(process)
		return err == nil && !alive
	})
}
