package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// launchCachePrune starts `sdd cache prune --scheduled` in dir as its own
// session with no terminal, so it outlives the command and nothing it does
// reaches the user's screen. Its output goes to logPath, replaced per run,
// which keeps a trace of the failures the run cannot record in its status.
func launchCachePrune(dir, logPath string) (err error) {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the sdd binary: %w", err)
	}
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("opening the cache pruning log: %w", err)
	}
	defer func() { err = errors.Join(err, log.Close()) }()
	cmd := exec.Command(exe, "cache", "prune", "--scheduled")
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped in the background so a long-running server leaves no zombie.
	go func() { _ = cmd.Wait() }()
	return nil
}
