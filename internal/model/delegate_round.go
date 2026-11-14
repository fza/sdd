package model

import "time"

// RoundOutcomeMargin is how long past its deadline a delegate round sdd
// launched may take to record its outcome before it counts as failed, and
// how long past its launch its process may take to be recorded as running
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
	// RoundNeverStarted is a round whose process was not recorded as running
	// within RoundOutcomeMargin of its launch.
	RoundNeverStarted
)

// PendingRound is a launched round that recorded no outcome, as a later call
// finds it.
type PendingRound struct {
	// Launched is when sdd recorded the round, before starting its process.
	Launched time.Time
	Deadline time.Time
	// Started is whether the round's process was recorded as running; Alive
	// whether it still runs.
	Started bool
	Alive   bool
}

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

// LapsedRound decides a launched round that recorded no outcome.
func LapsedRound(round PendingRound, now time.Time) RoundLapse {
	switch {
	case !round.Started && now.After(round.Launched.Add(RoundOutcomeMargin)):
		return RoundNeverStarted
	case !round.Started:
		return RoundRunning
	case !round.Alive:
		return RoundProcessGone
	case now.After(round.Deadline.Add(RoundOutcomeMargin)):
		return RoundPastTimeout
	}
	return RoundRunning
}
