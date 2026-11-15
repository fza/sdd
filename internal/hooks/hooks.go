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
)

// waitDelay bounds how long a stopped hook's leftover children may hold its
// output open.
const waitDelay = time.Second

// Commands implements the application's HookCommands port over the hooks
// declared in sources, read afresh on every call.
type Commands struct {
	sources Sources
}

// New returns Commands over the hooks sources declares.
func New(sources Sources) *Commands {
	return &Commands{sources: sources}
}

// Hooks returns the hooks as declared now.
func (c *Commands) Hooks(context.Context) ([]model.Hook, error) {
	return Declared(c.sources)
}

// Run runs hook's file with its args in its hooks folder, stdin on its
// standard input, and returns its stdout. When ctx ends, the hook's whole
// process group is killed.
func (c *Commands) Run(ctx context.Context, hook model.Hook, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, hook.Path, hook.Args...)
	cmd.Dir = hook.Origin.Dir
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
