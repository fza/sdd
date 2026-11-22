package query

import "github.com/networkteam/sdd/internal/model"

// GatherResult is one graph check: the reads as run, the entries opened by
// ID in full, and the candidates placed as marked lines (model.PlaceLines)
// with the set-aside groups.
type GatherResult struct {
	Graph  *model.Graph
	Reads  []model.GroundingRead
	Corpus *model.CorpusMark
	// Prefilter is the prefilter status: off until a prefilter runs.
	Prefilter string
	// PrefilterNote says what the prefilter did or why it did not run.
	PrefilterNote string
	// Found counts the candidates the reads found; Judged those a judgment
	// places, by the judge's score or a stored verdict.
	Found  int
	Judged int
	// Opened holds the entries opened by ID, every one in full.
	Opened   *ShowResult
	Lines    model.Lines
	SetAside []model.GatherSetAside
	// Held reports whether the consumer already holds an entry in full that
	// the serve did not open by ID: it prints as a pointer with the brief and
	// the way back, with the tag it is held under. Nil holds
	// nothing, as the CLI serves. Anchor places the tag in a body served in
	// full.
	Held   func(id string) (tag, next string, held bool)
	Anchor func(id, body string, field map[string]int) string
}
