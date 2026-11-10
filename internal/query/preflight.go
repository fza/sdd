// Package query holds domain query structs — pure read intent, processed by
// finders. Queries are plain data; no methods with side effects.
package query

import (
	"github.com/networkteam/sdd/internal/model"
)

// PreflightQuery captures intent to validate an entry against the graph using
// the pre-flight LLM validator. Pure read — no side effects of its own; the
// runner dependency injected into the finder handles the LLM call.
type PreflightQuery struct {
	Entry *model.Entry
	Model string // LLM model identifier (e.g. "claude-sonnet-4-6")
	// Light runs pre-flight as the light verifier over a grounding reading
	// (20261109-004729-d-cpt-xs5); nil runs it over the draft's refs alone.
	Light *LightPreflight
	// KindFit adds the check that the draft fits its kind; nil leaves it out.
	KindFit *model.KindFit
}

// LightPreflight is the grounding reading a light pre-flight judges the
// draft against, and the characters one call holds.
type LightPreflight struct {
	Full        []string
	Listed      []string
	SetAside    []model.GroundingVerdict
	BudgetChars int
}

// Severity classifies a pre-flight finding. The tooling layer decides what
// to block on (currently: only SeverityHigh blocks); templates describe
// severity in purely semantic terms and never name a threshold.
type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

// Finding is a single observation from pre-flight validation. The lowercase
// JSON tags are load-bearing: store values normalize through JSON, and the
// capture unit's template addresses findings by these keys.
type Finding struct {
	Severity    Severity `json:"severity"`
	Category    string   `json:"category"`
	Observation string   `json:"observation"`
	// Basis names the reading entries a light finding rests on.
	Basis []string `json:"basis,omitempty"`
}

// PreflightResult holds all findings from a pre-flight validator run.
// An empty Findings slice means the validator reported no findings.
type PreflightResult struct {
	Findings []Finding
	// Calls is how many light calls the reading took; zero outside light.
	Calls int
	// Excerpted names the reading entries a light call judged on excerpts.
	Excerpted []string
}

// HasBlocking reports whether any finding blocks entry creation. Currently
// only SeverityHigh blocks; this is the single source of truth for the
// blocking threshold (handler_new_entry uses it; templates do not).
func (r *PreflightResult) HasBlocking() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityHigh {
			return true
		}
	}
	return false
}
