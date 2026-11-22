package cliapp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/llm/claude"
)

// shutdownWindow bounds how long serve over stdio takes to exit once its
// context ends: Claude Code sends SIGKILL about 0.5 s after SIGINT.
const shutdownWindow = 400 * time.Millisecond

// serveStage names what serve is doing, for the error that names the work
// the shutdown window cut.
type serveStage struct {
	mu       sync.Mutex
	name     string
	children *claude.Children
}

func (s *serveStage) enter(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

func (s *serveStage) current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

type serveStageKey struct{}

// enterServeStage records the stage serve's ctx runs in; outside
// withShutdownWindow it does nothing.
func enterServeStage(ctx context.Context, name string) {
	if stage, ok := ctx.Value(serveStageKey{}).(*serveStage); ok {
		stage.enter(name)
	}
}

// servedChildren are the claude CLI calls ctx tracks, made by
// withShutdownWindow; a fresh set outside it.
func servedChildren(ctx context.Context) *claude.Children {
	if stage, ok := ctx.Value(serveStageKey{}).(*serveStage); ok {
		return stage.children
	}
	return claude.NewChildren()
}

// withShutdownWindow runs a stdio serve action until it returns or until
// window has passed since ctx ended, whichever comes first. Past the window it
// kills the claude CLI calls still running and returns an error naming the
// stage the action was still in, so no host kill cuts that work silently.
// Other transports run the action as it is.
func withShutdownWindow(window time.Duration, action cli.ActionFunc) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		children := claude.NewChildren()
		ctx = claude.WithChildren(ctx, children)
		if cmd.String("transport") != "stdio" {
			return action(context.WithValue(ctx, serveStageKey{}, &serveStage{name: "serving", children: children}), cmd)
		}
		return runWithinShutdownWindow(ctx, window, children, func(ctx context.Context) error { return action(ctx, cmd) })
	}
}

// errShutdownWindow marks the error of a serve the shutdown window cut.
var errShutdownWindow = errors.New("sdd serve: the shutdown window passed")

func runWithinShutdownWindow(ctx context.Context, window time.Duration, children *claude.Children, run func(context.Context) error) error {
	stage := &serveStage{name: "starting", children: children}
	done := make(chan error, 1)
	go func() { done <- run(context.WithValue(ctx, serveStageKey{}, stage)) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
	}
	cut := fmt.Errorf("%w %s after the stop, while %s; exiting with that work unfinished", errShutdownWindow, window, stage.current())
	expired, cancel := context.WithCancel(context.WithoutCancel(ctx))
	cancel()
	killErr := children.Kill(expired)
	if killErr != nil {
		return errors.Join(cut, killErr)
	}
	return cut
}
