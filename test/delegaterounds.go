package test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// roundWait bounds how long a test waits for a background delegate round.
const roundWait = 10 * time.Second

// ScriptedLauncher is a DelegateLauncher whose rounds the test drives: sdd
// runs each round in the background, the round waits until the test takes it
// with NextRound, and ends when the test calls Finish.
type ScriptedLauncher struct {
	// Reports is the runner every round reports it ran with.
	Reports string
	// LeaveUnconfigured leaves a delegate without a configured runner to the
	// harness, as the serve composition's launcher does.
	LeaveUnconfigured bool

	mu       sync.Mutex
	launches []sdd.DelegateLaunch
	rounds   chan *ScriptedRound
	stop     chan struct{}
}

// ScriptedRound is one background round waiting for the test to end it.
type ScriptedRound struct {
	Launch sdd.DelegateLaunch
	Round  sdd.DelegateRound
	end    chan roundEnd
}

type roundEnd struct {
	end sdd.DelegateRoundEnd
	err error
}

// NewScriptedLauncher returns a launcher reporting runner, whose rounds still
// waiting end when the test does.
func NewScriptedLauncher(t testing.TB, runner string) *ScriptedLauncher {
	l := &ScriptedLauncher{Reports: runner, rounds: make(chan *ScriptedRound, 8), stop: make(chan struct{})}
	t.Cleanup(func() { close(l.stop) })
	return l
}

func (l *ScriptedLauncher) LaunchDelegate(_ context.Context, launch sdd.DelegateLaunch) (sdd.DelegateRun, error) {
	if launch.Runner == "" && l.LeaveUnconfigured {
		return sdd.DelegateRun{}, sdd.ErrDelegateNotLaunched
	}
	l.mu.Lock()
	l.launches = append(l.launches, launch)
	l.mu.Unlock()
	round := func(ctx context.Context, round sdd.DelegateRound) (sdd.DelegateRoundEnd, error) {
		r := &ScriptedRound{Launch: launch, Round: round, end: make(chan roundEnd, 1)}
		select {
		case l.rounds <- r:
		case <-l.stop:
			return sdd.DelegateRoundEnd{}, errors.New("the test ended before the round was taken")
		}
		select {
		case end := <-r.end:
			return end.end, end.err
		case <-l.stop:
			return sdd.DelegateRoundEnd{}, errors.New("the test ended before the round did")
		case <-ctx.Done():
			return sdd.DelegateRoundEnd{}, ctx.Err()
		}
	}
	return sdd.DelegateRun{Runner: l.Reports, Round: round}, nil
}

// Launches lists every launch the launcher took on, in order.
func (l *ScriptedLauncher) Launches() []sdd.DelegateLaunch {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]sdd.DelegateLaunch(nil), l.launches...)
}

// NextRound waits for the next background round to start.
func (l *ScriptedLauncher) NextRound(t testing.TB) *ScriptedRound {
	t.Helper()
	select {
	case r := <-l.rounds:
		return r
	case <-time.After(roundWait):
		t.Fatal("no delegate round started in time")
		return nil
	}
}

// Finish ends the round, reporting the launcher's session or the error the
// round failed with.
func (r *ScriptedRound) Finish(session string, err error) {
	r.end <- roundEnd{end: sdd.DelegateRoundEnd{Session: session}, err: err}
}

// FinishSaying ends the round cleanly, reporting the launcher's session and
// the agent's last message.
func (r *ScriptedRound) FinishSaying(session, message string) {
	r.end <- roundEnd{end: sdd.DelegateRoundEnd{Session: session, Message: message}}
}

// Eventually waits until done holds, failing the test naming what when it
// does not in time. For outcomes a background round records after it ended.
func Eventually(t testing.TB, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(roundWait)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen in time", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
