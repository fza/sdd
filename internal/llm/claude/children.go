package claude

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"sync"
	"syscall"
)

// ErrChildrenKilled refuses a call once its Children were killed.
var ErrChildrenKilled = errors.New("the claude CLI calls of this process were killed")

// Children are the claude CLI calls running under one context, tracked by
// their process groups. A group outlives the process that started it, so a
// process about to exit kills them (Kill).
type Children struct {
	mu     sync.Mutex
	killed bool
	calls  map[int]chan struct{}
}

// NewChildren returns an empty set of calls.
func NewChildren() *Children {
	return &Children{calls: map[int]chan struct{}{}}
}

type childrenKey struct{}

// WithChildren has every call a Runner makes under the returned context
// tracked in children.
func WithChildren(ctx context.Context, children *Children) context.Context {
	return context.WithValue(ctx, childrenKey{}, children)
}

// Kill sends SIGKILL to the process group of every call still running and
// refuses each call from then on. It returns once every killed call
// returned, or with ctx's cause when ctx ends first.
func (c *Children) Kill(ctx context.Context) error {
	c.mu.Lock()
	c.killed = true
	running := maps.Clone(c.calls)
	c.mu.Unlock()
	var errs []error
	for group := range running {
		err := syscall.Kill(-group, syscall.SIGKILL)
		if err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("killing the claude CLI's process group %d: %w", group, err))
			delete(running, group)
		}
	}
	for group, ended := range running {
		select {
		case <-ended:
		case <-ctx.Done():
			return errors.Join(append(errs, fmt.Errorf("waiting for the claude CLI's process group %d: %w", group, context.Cause(ctx)))...)
		}
	}
	return errors.Join(errs...)
}

// start starts cmd as a tracked call; release ends its tracking once its Wait
// returned.
func (c *Children) start(cmd *exec.Cmd) (release func(), err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.killed {
		return nil, ErrChildrenKilled
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	group := cmd.Process.Pid
	ended := make(chan struct{})
	c.calls[group] = ended
	return func() {
		c.mu.Lock()
		delete(c.calls, group)
		c.mu.Unlock()
		close(ended)
	}, nil
}

// startCall starts cmd, tracked in the Children ctx carries.
func startCall(ctx context.Context, cmd *exec.Cmd) (release func(), err error) {
	children, _ := ctx.Value(childrenKey{}).(*Children)
	if children == nil {
		return func() {}, cmd.Start()
	}
	return children.start(cmd)
}
