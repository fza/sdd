package model

import (
	"strings"
	"time"
)

// TeamProofDir names the folder in the .sdd folder of the graph's
// repository that holds one committed proof record per judge.
const TeamProofDir = "proofs"

// TeamProof is the proof record a team shares for one judge
// (20261121-210755-d-tac-d5e): the judge and its prompt revision, the graph
// size the proof rests on, the thresholds it proved and each rule's step
// with its replay counts. It holds counts alone, no entry ID and no entry
// text. It applies to a machine with the same judge and revision while the
// graph has grown by less than a quarter (ExpiresAtSize).
type TeamProof struct {
	Judge         string     `json:"judge"`
	Revision      string     `json:"revision,omitempty"`
	GraphSize     int        `json:"graph_size"`
	ExpiresAtSize int        `json:"expires_at_size"`
	Thresholds    TeamCut    `json:"thresholds"`
	Rules         []TeamRule `json:"rules"`
	At            time.Time  `json:"at"`
}

// TeamCut is the threshold pair a team proof carries.
type TeamCut struct {
	Valid    bool    `json:"valid"`
	Stage    float64 `json:"stage,omitempty"`
	SetAside float64 `json:"set_aside,omitempty"`
	Relevant float64 `json:"relevant,omitempty"`
}

// TeamRule is one rule's step in a team proof and the replays behind it.
type TeamRule struct {
	Rule    HidingRule `json:"rule"`
	Step    HidingStep `json:"step"`
	Replays int        `json:"replays"`
	Needed  int        `json:"needed"`
	Hidden  int        `json:"hidden"`
	Bound   float64    `json:"bound"`
}

// TeamProofKey is the file name stem of judge's record: the judge's name
// with every character other than a lowercase letter, a digit, a dot or a
// hyphen turned into a hyphen.
func TeamProofKey(judge string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(CanonicalJudge(judge)))
}

// NewTeamProof is the record of a machine's own standing: its thresholds and
// each rule's local proof and step.
func NewTeamProof(judge, revision string, graphSize int, thresholds RelevanceThresholds, states []RuleState, at time.Time) TeamProof {
	team := TeamProof{
		Judge: CanonicalJudge(judge), Revision: revision, GraphSize: graphSize, ExpiresAtSize: expirySize(graphSize), At: at.UTC(),
		Thresholds: TeamCut{Valid: thresholds.Valid, Stage: thresholds.Stage, SetAside: thresholds.SetAside, Relevant: thresholds.Relevant},
	}
	for _, state := range states {
		proof := state.Proof
		if proof.Valid() && proof.GraphSize > 0 {
			team.GraphSize = min(team.GraphSize, proof.GraphSize)
			team.ExpiresAtSize = expirySize(team.GraphSize)
		}
		team.Rules = append(team.Rules, TeamRule{Rule: proof.Rule, Step: state.Step, Replays: proof.Checks, Needed: proof.Needed, Hidden: proof.Hidden, Bound: proof.Bound})
	}
	return team
}

func expirySize(graphSize int) int {
	return int(float64(graphSize)*ProofGrowth + 0.999999)
}

// Applies reports whether the record holds for judge at revision on a graph
// of graphSize live entries.
func (t TeamProof) Applies(judge, revision string, graphSize int) bool {
	return t.Judge == CanonicalJudge(judge) && t.Revision == revision && graphSize < t.ExpiresAtSize
}

// Rule returns the record's step for rule, compute-only when it holds none.
func (t TeamProof) Rule(rule HidingRule) TeamRule {
	for _, held := range t.Rules {
		if held.Rule == rule {
			return held
		}
	}
	return TeamRule{Rule: rule}
}

// TeamThresholds are the thresholds the record proved.
func (t TeamProof) TeamThresholds() RelevanceThresholds {
	return RelevanceThresholds{Valid: t.Thresholds.Valid, Stage: t.Thresholds.Stage, SetAside: t.Thresholds.SetAside, Relevant: t.Thresholds.Relevant}
}

// TeamProofDue reports whether a machine whose standing is current should
// write it over committed: when a rule's proof completed and no record is
// committed, and when a rule's step, the prompt revision or the record's
// expiry differs from the committed one. Counts alone moving write nothing.
func TeamProofDue(committed *TeamProof, current TeamProof, graphSize int) bool {
	if committed == nil {
		for _, rule := range current.Rules {
			if rule.Step == StepFull {
				return true
			}
		}
		return false
	}
	if committed.Judge != current.Judge || committed.Revision != current.Revision {
		return true
	}
	if graphSize >= committed.ExpiresAtSize && current.ExpiresAtSize != committed.ExpiresAtSize {
		return true
	}
	for _, rule := range HidingRules() {
		if committed.Rule(rule).Step != current.Rule(rule).Step {
			return true
		}
	}
	return false
}
