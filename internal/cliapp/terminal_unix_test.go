//go:build darwin || linux

package cliapp_test

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func openTerminalSide(t *testing.T, name string) *os.File {
	t.Helper()
	terminal, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open terminal side %s: %v", name, err)
	}
	t.Cleanup(func() { _ = terminal.Close() })
	if err := unix.IoctlSetWinsize(int(terminal.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 40, Col: 120}); err != nil {
		t.Fatalf("size terminal: %v", err)
	}
	return terminal
}
