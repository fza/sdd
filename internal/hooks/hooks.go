// Package hooks runs the hooks the person running sdd configured
// (20261109-005138-d-tac-i61): each command through `sh -c` in the project
// directory, the event's JSON payload on stdin.
package hooks

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// waitDelay bounds how long a stopped hook's leftover children may hold its
// output open.
const waitDelay = time.Second

// Commands implements the application's HookCommands port over a loader
// that reads the configured hooks afresh on every call.
type Commands struct {
	load func() ([]model.HookConfig, error)
	dir  string
}

// New returns Commands reading hooks through load and running them in dir.
func New(load func() ([]model.HookConfig, error), dir string) *Commands {
	return &Commands{load: load, dir: dir}
}

// Hooks returns the hooks as configured now.
func (c *Commands) Hooks(context.Context) ([]sdd.Hook, error) {
	configs, err := c.load()
	if err != nil {
		return nil, err
	}
	hooks := make([]sdd.Hook, 0, len(configs))
	for _, config := range configs {
		hooks = append(hooks, sdd.Hook{Event: config.Event, Command: config.Command, Timeout: time.Duration(config.Timeout) * time.Second, Findings: config.Findings})
	}
	return hooks, nil
}

// Run runs hook with stdin and returns its stdout. When ctx ends, the
// hook's whole process group is killed.
func (c *Commands) Run(ctx context.Context, hook sdd.Hook, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", hook.Command)
	cmd.Dir = c.dir
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	if err := cmd.Run(); err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
