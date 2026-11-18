package cliapp

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The server counts as idle once no tool call began within the quiet span:
// a call restarts the span, and a wait ends with its context.
func TestCallActivity_WaitsForAQuietSpan(t *testing.T) {
	activity := &callActivity{now: time.Now, quiet: 150 * time.Millisecond, poll: 5 * time.Millisecond}
	activity.touch()
	started := time.Now()
	stop := make(chan struct{})
	go func() {
		for range 3 {
			time.Sleep(40 * time.Millisecond)
			activity.touch()
		}
		close(stop)
	}()
	if err := activity.waitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-stop
	if waited := time.Since(started); waited < 250*time.Millisecond {
		t.Fatalf("waited %v: each call should restart the quiet span", waited)
	}

	activity.touch()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := activity.waitIdle(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait = %v: a wait should end with its context", err)
	}
}
