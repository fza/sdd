package model

// Phase names the active stage of a long-running command so the transient
// footer can label it truthfully as work moves between stages. Handlers report
// transitions through OnPhase-style callbacks; the output coordinator arms on a
// reported phase and the footer label derives from it — never a bare call-site
// string decided mid-run (s-tac-jbm). Rendering lives in internal/cliout; this
// is the dependency-free vocabulary both sides share.
type Phase string

const (
	PhaseConnecting  Phase = "connecting"  // establishing a connection / first cache clone
	PhaseCloning     Phase = "cloning"     // a git clone is running
	PhaseSyncing     Phase = "syncing"     // a cache pull / freshen is running
	PhaseIndexing    Phase = "indexing"    // embedding / index fill is running
	PhaseSummarizing Phase = "summarizing" // summary and title generation is running
)

// ScoringPhase is the phase of judge scoring labelled pairs, named as
// grounding status prints the judge.
func ScoringPhase(judge string) Phase { return Phase(judge) }

// Counted reports whether the phase works through a known number of units, so
// the footer shows a determinate bar for it rather than the spinner alone:
// every phase but those reaching a repository.
func (p Phase) Counted() bool {
	switch p {
	case "", PhaseConnecting, PhaseCloning, PhaseSyncing:
		return false
	}
	return true
}
