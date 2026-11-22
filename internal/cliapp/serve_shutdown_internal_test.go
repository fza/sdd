package cliapp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/llm/claude"
)

// A serve whose background work ignores the stop returns within the
// shutdown window, naming the stage it was cut in; one whose work ends on
// the stop returns as soon as it ends.
func TestRunWithinShutdownWindowBoundsTheExit(t *testing.T) {
	const window = 100 * time.Millisecond
	tests := []struct {
		name    string
		work    func(ctx context.Context, release <-chan struct{})
		cut     bool
		longest time.Duration
	}{
		{
			name: "work ignoring the stop",
			work: func(_ context.Context, release <-chan struct{}) { <-release },
			cut:  true, longest: window + 2*time.Second,
		},
		{
			name:    "work ending on the stop",
			work:    func(ctx context.Context, _ <-chan struct{}) { <-ctx.Done() },
			longest: window,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, stop := context.WithCancel(t.Context())
			release := make(chan struct{})
			defer close(release)
			started := make(chan struct{})
			returned := make(chan error, 1)
			go func() {
				returned <- runWithinShutdownWindow(ctx, window, claude.NewChildren(), func(ctx context.Context) error {
					enterServeStage(ctx, "waiting for the fake background task")
					close(started)
					tt.work(ctx, release)
					return ctx.Err()
				})
			}()
			<-started
			stopped := time.Now()
			stop()
			var err error
			select {
			case err = <-returned:
			case <-time.After(10 * time.Second):
				t.Fatal("serve should return after the stop")
			}
			took := time.Since(stopped)
			if tt.cut {
				if took < window {
					t.Errorf("the cut serve should wait out the window, returned after %s", took)
				}
				if !errors.Is(err, errShutdownWindow) || !strings.Contains(err.Error(), "waiting for the fake background task") {
					t.Errorf("the cut serve should name the stage it was cut in, got %v", err)
				}
			} else if errors.Is(err, errShutdownWindow) {
				t.Errorf("work ending on the stop should not be cut, got %v", err)
			}
			if took > tt.longest {
				t.Errorf("serve should return within %s of the stop, took %s", tt.longest, took)
			}
		})
	}
}
