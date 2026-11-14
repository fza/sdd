package model

import "time"

// RoundOutcomeMargin is how long past its deadline a delegate round sdd
// launched may take to record its outcome before it counts as failed
// (20261114-160323-d-tac-n0n).
const RoundOutcomeMargin = time.Minute

// RoundLapse is why a launched round that recorded no outcome failed, or
// RoundRunning while it has not.
type RoundLapse int

const (
	RoundRunning RoundLapse = iota
	// RoundProcessGone is a round whose process ended without an outcome.
	RoundProcessGone
	// RoundPastTimeout is a round whose deadline plus RoundOutcomeMargin passed.
	RoundPastTimeout
)

// RoundDeadline is when a launched round must have ended: timeout after it
// started running, which is its launch or, when later, the end of the round
// before it, which it waits for.
func RoundDeadline(launched, previousEnded time.Time, timeout time.Duration) time.Time {
	began := launched
	if previousEnded.After(began) {
		began = previousEnded
	}
	return began.Add(timeout)
}

// LapsedRound decides a launched round that recorded no outcome: alive is
// whether its process still runs.
func LapsedRound(alive bool, deadline, now time.Time) RoundLapse {
	switch {
	case !alive:
		return RoundProcessGone
	case now.After(deadline.Add(RoundOutcomeMargin)):
		return RoundPastTimeout
	}
	return RoundRunning
}
