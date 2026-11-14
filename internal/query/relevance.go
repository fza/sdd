package query

// LeftoverRelevanceQuery asks which relevance ledger segments the project's
// .sdd/ folder at SDDDir still holds. SDDDir empty asks nothing.
type LeftoverRelevanceQuery struct {
	SDDDir string
}

// LeftoverRelevanceResult names the folder under .sdd/ and the segment
// files in it, which sdd ignores since the ledger lives only in machine
// state (20261114-105752-d-tac-tnk). No segments means nothing to report.
type LeftoverRelevanceResult struct {
	Dir      string
	Segments []string
}
