// Package roundproc starts the detached processes the delegate rounds sdd
// launches run in, and tells and stops such a process later from any process
// (20261114-160323-d-tac-n0n).
package roundproc

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// Process is a running process: its PID and its start stamp, which tells it
// from a later process that took the same PID.
type Process struct {
	PID     int
	Started string
}

// Start starts name with args in dir as the leader of a session and process
// group of its own, with no stdio, and returns it once it runs.
func Start(dir, name string, args ...string) (Process, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return Process{}, err
	}
	pid := cmd.Process.Pid
	started, running, err := startStamp(pid)
	// Reaped in the background so a long-running server leaves no zombie.
	go func() { _ = cmd.Wait() }()
	if err == nil && !running {
		err = errors.New("it ended at once")
	}
	if err != nil {
		return Process{}, errors.Join(fmt.Errorf("reading the start of process %d: %w", pid, err), syscall.Kill(-pid, syscall.SIGKILL))
	}
	return Process{PID: pid, Started: started}, nil
}

// Alive reports whether process still runs: no process with its PID, a
// process that ended and was not reaped yet, or one that started at another
// time is gone.
func Alive(process Process) (bool, error) {
	started, running, err := startStamp(process.PID)
	return running && started == process.Started, err
}

// Stop kills a running process with its whole process group; a process
// already gone is left alone.
func Stop(process Process) error {
	alive, err := Alive(process)
	if err != nil || !alive {
		return err
	}
	err = syscall.Kill(-process.PID, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
