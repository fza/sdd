package application

import (
	"context"
	"sync"
)

// background is the work an Application runs past the call that starts it.
type background struct {
	mu      sync.Mutex
	stopped bool
	running sync.WaitGroup
}

// start runs work in the background; once stop began, it runs nothing.
func (b *background) start(work func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return
	}
	b.running.Go(work)
}

func (b *background) stop(ctx context.Context) error {
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
	done := make(chan struct{})
	go func() {
		b.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// Shutdown waits for the work the application runs past the calls that
// started it, each title backfill, and starts no more of it. It returns
// ctx's cause when ctx ends first.
func (a *Application) Shutdown(ctx context.Context) error {
	return a.background.stop(ctx)
}
