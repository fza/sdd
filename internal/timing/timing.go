// Package timing reports how long each phase of a command takes, one stderr
// line per finished phase, so a slow command can be broken down on the
// machine where it is slow. A context without a Recorder records nothing.
package timing

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

type Recorder struct {
	mu     sync.Mutex
	writer io.Writer
	now    func() time.Time
	start  time.Time
}

func NewRecorder(writer io.Writer, now func() time.Time) *Recorder {
	return &Recorder{writer: writer, now: now, start: now()}
}

type recorderKey struct{}

func WithRecorder(ctx context.Context, recorder *Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, recorder)
}

// Start begins a phase and returns the function that ends it. Phases may
// nest and overlap; each line carries the phase's offset from the
// recorder's start so concurrent phases can be told apart.
func Start(ctx context.Context, phase string) func() {
	recorder, _ := ctx.Value(recorderKey{}).(*Recorder)
	if recorder == nil {
		return func() {}
	}
	began := recorder.now()
	return func() {
		ended := recorder.now()
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		_, _ = fmt.Fprintf(recorder.writer, "timing: %-24s %10.1fms  at %8.1fms\n", phase, milliseconds(ended.Sub(began)), milliseconds(began.Sub(recorder.start)))
	}
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
