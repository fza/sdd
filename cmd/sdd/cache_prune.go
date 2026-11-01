package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// launchCachePrune starts `sdd cache prune --scheduled` in dir as its own
// session with no terminal and no output, so it outlives the command and
// nothing it does reaches the user's screen.
func launchCachePrune(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the sdd binary: %w", err)
	}
	cmd := exec.Command(exe, "cache", "prune", "--scheduled")
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped in the background so a long-running server leaves no zombie.
	go func() { _ = cmd.Wait() }()
	return nil
}
