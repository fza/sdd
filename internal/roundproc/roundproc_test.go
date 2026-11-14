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
	process, err := roundproc.Start(dir, filepath.Join(dir, "round.log"), "/bin/sh", "-c", "sleep 30 & echo $! > child; wait")
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

	if err := roundproc.Stop(process, roundproc.Process{}); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the process ending", func() bool {
		alive, err := roundproc.Alive(process)
		return err == nil && !alive
	})
	eventually(t, "the process's child ending", func() bool {
		return errors.Is(syscall.Kill(child, 0), syscall.ESRCH)
	})
	if err := roundproc.Stop(process, roundproc.Process{}); err != nil {
		t.Fatalf("stopping a process already gone should leave it alone, got %v", err)
	}
}

// A process that ended on its own is gone.
func TestAProcessThatEndedIsGone(t *testing.T) {
	process, err := roundproc.Start(t.TempDir(), filepath.Join(t.TempDir(), "round.log"), "/bin/sh", "-c", "sleep 0.2")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the process ending", func() bool {
		alive, err := roundproc.Alive(process)
		return err == nil && !alive
	})
}

// orphanedGroup starts a process that starts a child in its group and ends,
// leaving the child running there, and returns both.
func orphanedGroup(t *testing.T) (leader, child roundproc.Process) {
	t.Helper()
	dir := t.TempDir()
	leader, err := roundproc.Start(dir, filepath.Join(dir, "round.log"), "/bin/sh", "-c", "sleep 30 & echo $! > child.tmp && mv child.tmp child")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-leader.PID, syscall.SIGKILL) })
	var pid int
	eventually(t, "the process starting its child", func() bool {
		out, err := os.ReadFile(filepath.Join(dir, "child"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(out)))
		return err == nil && pid > 0
	})
	eventually(t, "the process ending", func() bool {
		alive, err := roundproc.Alive(leader)
		return err == nil && !alive
	})
	child, err = roundproc.Identify(pid)
	if err != nil {
		t.Fatal(err)
	}
	return leader, child
}

// The group of a process that is gone is killed only while the member it was
// handed still runs in it.
func TestStopEndsTheGroupOfAGoneProcessOnlyThroughItsMember(t *testing.T) {
	tests := []struct {
		name   string
		member func(t *testing.T, child roundproc.Process) roundproc.Process
		ends   bool
	}{
		{name: "member runs in the group", ends: true, member: func(_ *testing.T, child roundproc.Process) roundproc.Process { return child }},
		{name: "no member", member: func(*testing.T, roundproc.Process) roundproc.Process { return roundproc.Process{} }},
		{name: "member started at another time", member: func(_ *testing.T, child roundproc.Process) roundproc.Process {
			return roundproc.Process{PID: child.PID, Started: "another start"}
		}},
		{name: "member runs in another group", member: func(t *testing.T, _ roundproc.Process) roundproc.Process {
			other, err := roundproc.Start(t.TempDir(), filepath.Join(t.TempDir(), "round.log"), "/bin/sh", "-c", "sleep 30")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Kill(-other.PID, syscall.SIGKILL) })
			return other
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leader, child := orphanedGroup(t)
			member := tt.member(t, child)

			if err := roundproc.Stop(leader, member); err != nil {
				t.Fatal(err)
			}

			if tt.ends {
				eventually(t, "the child ending with its group", func() bool {
					alive, err := roundproc.Alive(child)
					return err == nil && !alive
				})
				return
			}
			time.Sleep(100 * time.Millisecond)
			if alive, err := roundproc.Alive(child); err != nil || !alive {
				t.Fatalf("the group should be left alone, got the child running %v, %v", alive, err)
			}
			if member.PID != 0 && member.PID != child.PID {
				if alive, err := roundproc.Alive(member); err != nil || !alive {
					t.Fatalf("a member of another group should be left alone, got it running %v, %v", alive, err)
				}
			}
		})
	}
}

// A process that runs is identified by its start stamp; one that ended is
// refused as ended.
func TestIdentify(t *testing.T) {
	process, err := roundproc.Start(t.TempDir(), filepath.Join(t.TempDir(), "round.log"), "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-process.PID, syscall.SIGKILL) })

	identified, err := roundproc.Identify(process.PID)

	if err != nil || identified != process {
		t.Fatalf("a running process should be identified as started, got %+v, %v", identified, err)
	}
	if err := roundproc.Stop(process, roundproc.Process{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the process ending", func() bool {
		_, err := roundproc.Identify(process.PID)
		return errors.Is(err, roundproc.ErrEnded)
	})
}
