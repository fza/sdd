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
	// life ends once stop began; made on first use, so the zero value is
	// ready.
	life    context.Context
	endLife context.CancelFunc
}

// start runs work in the background and reports whether it did; once stop
// began, it runs nothing.
func (b *background) start(work func()) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return false
	}
	b.running.Go(work)
	return true
}

// detached is ctx with its values but without its cancellation, for work that
// outlives the call ctx belongs to; it ends once stop begins instead. release
// frees it.
func (b *background) detached(ctx context.Context) (detached context.Context, release context.CancelFunc) {
	b.mu.Lock()
	b.init()
	life := b.life
	b.mu.Unlock()
	detached, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(life, cancel)
	return detached, func() {
		stop()
		cancel()
	}
}

func (b *background) init() {
	if b.life == nil {
		b.life, b.endLife = context.WithCancel(context.Background())
	}
}

// stop starts no more work, ends every detached context and waits for the
// running work, returning ctx's cause when ctx ends first.
func (b *background) stop(ctx context.Context) error {
	b.mu.Lock()
	b.stopped = true
	b.init()
	b.endLife()
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

// Shutdown ends each title backfill, waits for the work the application runs
// past the calls that started it, each sync round a call stopped waiting for
// and each backfill, and starts no more of it. It returns ctx's cause when
// ctx ends first.
func (a *Application) Shutdown(ctx context.Context) error {
	return a.background.stop(ctx)
}
