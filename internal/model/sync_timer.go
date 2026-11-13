package model

// TimerRound is what the serve timer runs for an sdd-owned clone
// (20261113-232855-d-tac-1nz).
type TimerRound string

const (
	TimerRoundNone TimerRound = ""
	// TimerRoundSync is the clone's sync round.
	TimerRoundSync TimerRound = "sync"
	// TimerRoundPush pushes the commits waiting, without a fetch.
	TimerRoundPush TimerRound = "push"
)

// PlanTimerRound fetches only while the clone's lease holds and its fetch is
// due; commits waiting are pushed once their push is due, lease or not.
func PlanTimerRound(leased, fetchDue, pushDue bool) TimerRound {
	switch {
	case leased && fetchDue:
		return TimerRoundSync
	case pushDue:
		return TimerRoundPush
	default:
		return TimerRoundNone
	}
}
