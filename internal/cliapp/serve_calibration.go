package cliapp

import (
	"context"
	"sync/atomic"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// A served project's background calibration starts serveCalibrationDelay
// after the server, so the session's first calls go first. Past its first
// stage it scores only while the server is idle: no tool call began within
// serveIdleAfter, looked at every serveIdlePoll.
const (
	serveCalibrationDelay = time.Minute
	serveIdleAfter        = time.Minute
	serveIdlePoll         = 5 * time.Second
)

// callActivity is when the server last began a tool call.
type callActivity struct {
	last        atomic.Int64
	now         func() time.Time
	quiet, poll time.Duration
}

func newCallActivity() *callActivity {
	activity := &callActivity{now: time.Now, quiet: serveIdleAfter, poll: serveIdlePoll}
	activity.touch()
	return activity
}

func (a *callActivity) touch() {
	a.last.Store(a.now().UnixNano())
}

// waitIdle blocks until no tool call began within the quiet span, or ctx
// ends.
func (a *callActivity) waitIdle(ctx context.Context) error {
	for {
		since := a.now().Sub(time.Unix(0, a.last.Load()))
		if since >= a.quiet {
			return nil
		}
		timer := time.NewTimer(min(a.poll, a.quiet-since))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// runServedCalibration runs the background calibration of project after
// serveCalibrationDelay until ctx ends.
func runServedCalibration(ctx context.Context, application *sdd.Application, identity sdd.RequestIdentity, project sdd.ProjectID, activity *callActivity) {
	timer := time.NewTimer(serveCalibrationDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	application.AutoCalibrate(ctx, identity, project, sdd.AutoCalibrationRequest{WaitIdle: activity.waitIdle})
}
