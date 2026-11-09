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
// declared in sources, read afresh on every call, and the confirmations in
// store.
type Commands struct {
	sources Sources
	store   *Store
}

// New returns Commands over the hooks sources declares.
func New(sources Sources, store *Store) *Commands {
	return &Commands{sources: sources, store: store}
}

// Hooks returns the hooks as declared now, hashed, with their state.
func (c *Commands) Hooks(context.Context) ([]model.Hook, error) {
	declared, err := Declared(c.sources)
	if err != nil {
		return nil, err
	}
	return c.store.Resolve(declared)
}

// Review returns what the person sees before confirming hook.
func (c *Commands) Review(_ context.Context, hook model.Hook) (model.HookReview, error) {
	return Review(hook)
}

// Confirm records hook's hash as confirmed.
func (c *Commands) Confirm(_ context.Context, hook model.Hook) error {
	return c.store.Confirm(hook)
}

// Run runs hook's file with its args in its hooks folder, stdin on its
// standard input, and returns its stdout. It hashes the hook again first and
// refuses one whose files changed since its hash was read. When ctx ends, the
// hook's whole process group is killed.
func (c *Commands) Run(ctx context.Context, hook model.Hook, stdin []byte) ([]byte, error) {
	hash, _, err := Hash(hook)
	if err != nil {
		return nil, err
	}
	if hash != hook.Hash {
		return nil, fmt.Errorf("%s changed while the call ran; call again to resolve it anew", hook.Label())
	}
	cmd := exec.CommandContext(ctx, hook.Path, hook.Args...)
	cmd.Dir = hook.Origin.Dir
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
