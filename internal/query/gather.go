package query

import "github.com/networkteam/sdd/internal/model"

// GatherFullCap bounds the entries one gather serves in full (D35); the rest
// list as lines with the call that opens them.
const GatherFullCap = 12

// GatherResult is one gather serve (20261109-004506-d-cpt-udz, 5.3): the
// reads as run, the entries served in full, the candidates to judge and the
// entries set aside.
type GatherResult struct {
	Graph  *model.Graph
	Reads  []model.GroundingRead
	Corpus *model.CorpusMark
	// Prefilter is the prefilter status: off until a prefilter runs.
	Prefilter string
	// Full holds the entries served in full, at most GatherFullCap; Overflow
	// the opened entries past the cap.
	Full     *ShowResult
	Overflow []*model.Entry
	ToJudge  []model.GatherCandidate
	SetAside []model.GatherSetAside
	// Held names the candidates the consumer already holds in full: they
	// print as a pointer with the brief and the way back (D34).
	Held map[string]bool
}
