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

// state is what inspect reads of a process: whether it runs, and while it
// does its start stamp and its process group.
type state struct {
	running bool
	started string
	group   int
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
	process, err := Identify(pid)
	// Reaped in the background so a long-running server leaves no zombie.
	go func() { _ = cmd.Wait() }()
	if err != nil {
		return Process{}, errors.Join(err, syscall.Kill(-pid, syscall.SIGKILL))
	}
	return process, nil
}

// ErrEnded is what Identify fails with for a process that ended.
var ErrEnded = errors.New("the process ended")

// Identify reads the running process with pid.
func Identify(pid int) (Process, error) {
	info, err := inspect(pid)
	if err != nil {
		return Process{}, fmt.Errorf("reading the start of process %d: %w", pid, err)
	}
	if !info.running {
		return Process{}, fmt.Errorf("reading the start of process %d: %w", pid, ErrEnded)
	}
	return Process{PID: pid, Started: info.started}, nil
}

// Alive reports whether process still runs: no process with its PID, a
// process that ended and was not reaped yet, or one that started at another
// time is gone.
func Alive(process Process) (bool, error) {
	info, err := inspect(process.PID)
	return info.running && info.started == process.Started, err
}

// Stop kills a running process with its whole process group. A process
// already gone leaves its group to member, a process it started in that
// group, zero when none: the group is killed only while member still runs in
// it, since no new group takes the ID while a member lives.
func Stop(process, member Process) error {
	alive, err := Alive(process)
	if err != nil {
		return err
	}
	if !alive {
		inGroup, err := runsInGroup(member, process.PID)
		if err != nil || !inGroup {
			return err
		}
	}
	err = syscall.Kill(-process.PID, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func runsInGroup(member Process, group int) (bool, error) {
	if member.PID == 0 {
		return false, nil
	}
	info, err := inspect(member.PID)
	return info.running && info.started == member.Started && info.group == group, err
}
