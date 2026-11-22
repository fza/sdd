package model

import (
	"cmp"
	"maps"
	"regexp"
	"slices"
	"time"
)

// HidingRule names a rule that hides candidates in a graph check: the
// learned set-aside threshold (Calibrate) or the far floor (FarFloor).
type HidingRule string

const (
	RuleThreshold HidingRule = "threshold"
	RuleFarFloor  HidingRule = "far-floor"
)

// HidingRules lists the rules in the order a status shows them.
func HidingRules() []HidingRule { return []HidingRule{RuleThreshold, RuleFarFloor} }

// HidingStep is how far a rule may hide (20261121-210755-d-tac-d5e): at
// compute-only it computes what it would hide and hides nothing; narrowed,
// the threshold sets aside below half its cut and the far floor takes its
// careful form; at full the threshold takes its cut and the far floor its
// median form.
type HidingStep uint8

const (
	StepComputeOnly HidingStep = iota
	StepNarrowed
	StepFull
)

// String names the step as a status shows it.
func (s HidingStep) String() string {
	switch s {
	case StepNarrowed:
		return "narrowed"
	case StepFull:
		return "full"
	}
	return "compute-only"
}

// Proof constants: the Wilson bounds the steps rest on, the growth of the
// graph a proof outlives, and the misses from distinct captures that take a
// rule one step down.
const (
	ProofFull     = RecallFloor
	ProofNarrowed = 0.90
	ProofGrowth   = 1.25
	MissesPerStep = 3
)

// MissDetector names what noticed a miss.
type MissDetector string

const (
	// DetectRescue: the finished entry references an entry a rule hid in
	// its own check.
	DetectRescue MissDetector = "rescue"
	// DetectVerifier: a finding of the deep verifier rests on an entry the
	// drafter's check hid.
	DetectVerifier MissDetector = "verifier"
	// DetectScan: the scan after writing found a hidden entry that
	// duplicates or contradicts the written one.
	DetectScan MissDetector = "scan"
	// DetectPerson: the person's own words during the capture name a
	// hidden entry.
	DetectPerson MissDetector = "person"
)

// Miss is a needed entry a rule hid in a capture's check, as a detector
// recorded it.
type Miss struct {
	Capture  string       `json:"capture"`
	Hidden   string       `json:"hidden"`
	Rule     HidingRule   `json:"rule"`
	Detector MissDetector `json:"detector"`
	Judge    string       `json:"judge"`
	At       time.Time    `json:"at"`
}

// CheckTally is what a published capture's check did per rule: the step it
// held, the entries it hid, highest score first, and how many it would have
// hidden at the full step. Margins are the scores of the entries the capture
// references minus the threshold's cut, recorded while the judge held one;
// they warn and move no rule.
type CheckTally struct {
	Capture string      `json:"capture"`
	Judge   string      `json:"judge"`
	At      time.Time   `json:"at"`
	Rules   []RuleTally `json:"rules,omitempty"`
	Margins []float64   `json:"margins,omitempty"`
}

// RuleTally is one rule's share of a CheckTally.
type RuleTally struct {
	Rule   HidingRule `json:"rule"`
	Step   HidingStep `json:"step"`
	Hidden []string   `json:"hidden,omitempty"`
	Would  int        `json:"would,omitempty"`
}

// Scanned marks a capture whose scan after writing ran.
type Scanned struct {
	Capture string    `json:"capture"`
	At      time.Time `json:"at"`
}

// ProofRecords is the fold of the machine's proof records for one project:
// replayed checks, check tallies, scans and misses.
type ProofRecords struct {
	Replays []ReplayCheck
	Tallies []CheckTally
	Scans   []Scanned
	Misses  []Miss
}

// HidingProof is what a rule's replayed checks prove for a judge at a prompt
// revision: the needed entries they held and those the rule hid, the
// Wilson lower bound (95%) of the share not hidden, the graph size the
// proof rests on, the smallest of its checks', and its latest check. Expired
// reports replays of the judge that no longer count: a changed prompt or a
// graph grown by a quarter since.
type HidingProof struct {
	Rule      HidingRule
	Judge     string
	Revision  string
	GraphSize int
	Checks    int
	Needed    int
	Hidden    int
	Bound     float64
	Latest    time.Time
	Expired   bool
}

// Valid reports a proof resting on at least one replayed check.
func (p HidingProof) Valid() bool { return p.Checks > 0 && p.Needed > 0 }

// ValidReplays returns the replays that count for judge at revision on a
// graph of graphSize live entries: same judge and revision, from a graph
// that has not grown by a quarter since.
func ValidReplays(checks []ReplayCheck, judge, revision string, graphSize int) []ReplayCheck {
	return slices.DeleteFunc(slices.Clone(checks), func(check ReplayCheck) bool {
		return check.Judge != judge || check.Revision != revision || float64(graphSize) >= float64(check.GraphSize)*ProofGrowth
	})
}

// ProveRule folds judge's replayed checks into the proof of rule. The
// threshold hides a needed candidate scoring below thresholds' cut, and
// proves nothing while they are invalid; the far floor hides one at or
// below the median line of its call that it does not spare, and checks
// without far entries do not count for it.
func ProveRule(rule HidingRule, checks []ReplayCheck, judge, revision string, graphSize int, thresholds RelevanceThresholds) HidingProof {
	proof := HidingProof{Rule: rule, Judge: judge, Revision: revision}
	valid := ValidReplays(checks, judge, revision, graphSize)
	proof.Expired = len(valid) == 0 && slices.ContainsFunc(checks, func(check ReplayCheck) bool { return check.Judge == judge })
	if rule == RuleThreshold && !thresholds.Valid {
		return proof
	}
	for _, check := range valid {
		counted := false
		for _, candidate := range check.Candidates {
			if !candidate.Needed || rule == RuleFarFloor && !candidate.Far {
				continue
			}
			counted = true
			proof.Needed++
			if replayHides(rule, candidate, thresholds) {
				proof.Hidden++
			}
		}
		if !counted {
			continue
		}
		proof.Checks++
		if proof.GraphSize == 0 || check.GraphSize < proof.GraphSize {
			proof.GraphSize = check.GraphSize
		}
		if check.At.After(proof.Latest) {
			proof.Latest = check.At
		}
	}
	proof.Bound = WilsonLower(proof.Needed-proof.Hidden, proof.Needed)
	return proof
}

func replayHides(rule HidingRule, candidate ReplayCandidate, thresholds RelevanceThresholds) bool {
	if rule == RuleFarFloor {
		return candidate.Far && !candidate.Spared && candidate.Score <= candidate.Median
	}
	return ScoreValue(candidate.Score) < thresholds.SetAside
}

// ProvenStep is the step a proof's bound gives: full at ProofFull, narrowed
// at ProofNarrowed, else compute-only. The far floor holds narrowed without
// a proof: its careful form rests on the measurement shipped with sdd.
func ProvenStep(proof HidingProof) HidingStep {
	step := StepComputeOnly
	switch {
	case proof.Valid() && proof.Bound >= ProofFull:
		step = StepFull
	case proof.Valid() && proof.Bound >= ProofNarrowed:
		step = StepNarrowed
	}
	if proof.Rule == RuleFarFloor {
		step = max(step, StepNarrowed)
	}
	return step
}

// RuleState is a rule's standing for a judge: its proof, the misses
// recorded since the proof's latest check, from how many captures, and the
// step it holds.
type RuleState struct {
	Proof    HidingProof
	Misses   int
	Captures int
	Step     HidingStep
	// Team reports a step taken from the committed team record while the
	// machine's own replays prove nothing yet (ApplyTeamProof).
	Team bool
}

// HidingState folds the proof of rule and judge's misses since its latest
// replay into the rule's step: one step down for every MissesPerStep misses
// from as many captures, never below compute-only. Only a fresh replay
// lifts it again.
func HidingState(rule HidingRule, records ProofRecords, judge, revision string, graphSize int, thresholds RelevanceThresholds) RuleState {
	state := RuleState{Proof: ProveRule(rule, records.Replays, judge, revision, graphSize, thresholds)}
	state.stepDown(ProvenStep(state.Proof), rule, records, judge, state.Proof.Latest)
	return state
}

// ApplyTeamProof lets team, the committed record, set the step of a rule
// whose local replays prove nothing yet, where it applies to judge at
// revision on a graph of graphSize: the record's step, taken down by the
// misses since it was written. A local proof always wins.
func ApplyTeamProof(state RuleState, team *TeamProof, records ProofRecords, judge, revision string, graphSize int) RuleState {
	if state.Proof.Valid() || team == nil || !team.Applies(judge, revision, graphSize) {
		return state
	}
	rule := state.Proof.Rule
	state.Team = true
	state.stepDown(team.Rule(rule).Step, rule, records, judge, team.At)
	return state
}

// stepDown sets the step from base, one step lower for every
// MissesPerStep misses of rule from as many captures after since.
func (s *RuleState) stepDown(base HidingStep, rule HidingRule, records ProofRecords, judge string, since time.Time) {
	s.Misses = 0
	captures := map[string]bool{}
	for _, miss := range records.Misses {
		if miss.Rule == rule && CanonicalJudge(miss.Judge) == CanonicalJudge(judge) && miss.At.After(since) {
			s.Misses++
			captures[miss.Capture] = true
		}
	}
	s.Captures = len(captures)
	s.Step = HidingStep(max(int(base)-s.Captures/MissesPerStep, int(StepComputeOnly)))
}

// SteppedThresholds returns thresholds as a threshold at step may hide
// with: the cut as calibrated at full, half of it narrowed, none at
// compute-only. The serve-in-full threshold stays.
func SteppedThresholds(thresholds RelevanceThresholds, step HidingStep) RelevanceThresholds {
	switch step {
	case StepNarrowed:
		thresholds.SetAside /= 2
	case StepComputeOnly:
		thresholds.SetAside = 0
	}
	return thresholds
}

// FloorForm is the form of the far floor's line at step: median at full,
// careful below.
func FloorForm(step HidingStep) FarForm {
	if step == StepFull {
		return FarMedian
	}
	return FarCareful
}

// DetectionRate is the share of the planted misses among judge's valid
// replays the scan after writing caught, and how many were planted.
func DetectionRate(checks []ReplayCheck) (rate float64, planted int) {
	caught := 0
	for _, check := range checks {
		if check.Planted == "" {
			continue
		}
		planted++
		if check.Caught {
			caught++
		}
	}
	if planted > 0 {
		rate = float64(caught) / float64(planted)
	}
	return rate, planted
}

// CaptureEvidence is what a published capture shows of the entries it
// needed: those it answers (refs, supersedes, closes), the findings of its
// deep verifier, and the person's own words relayed during it.
type CaptureEvidence struct {
	Answered []string
	Findings []DeepFinding
	Words    []string
}

// CaptureProofs returns the proof records a published capture adds, per
// judge whose scores its reading holds: the tally of its check, the margins
// of the entries it answers above the threshold's cut, and a miss for each
// entry a rule hid in its check that the capture still needed, recorded by
// the first detector that saw it: rescue for an entry it answers, verifier
// for one a finding rests on, person for one the person's words name by
// full or short ID.
func CaptureProofs(entryID string, reading *Reading, evidence CaptureEvidence, at time.Time) ProofRecords {
	var records ProofRecords
	if reading == nil {
		return records
	}
	answered := evidence.Answered
	var basis []string
	for _, finding := range evidence.Findings {
		basis = append(basis, finding.Basis...)
	}
	named := NamedEntries(evidence.Words)
	for _, judge := range slices.Sorted(maps.Keys(reading.scores)) {
		held := reading.scores[judge]
		tally := CheckTally{Capture: entryID, Judge: judge, At: at}
		missed := map[string]bool{}
		for _, rule := range HidingRules() {
			hidden := held.hidden(rule)
			mark, marked := held.hiding[rule]
			if !marked && len(hidden) == 0 {
				continue
			}
			tally.Rules = append(tally.Rules, RuleTally{Rule: rule, Step: mark.Step, Hidden: hidden, Would: mark.Would})
			for _, id := range hidden {
				detector := MissDetector("")
				switch {
				case slices.Contains(answered, id):
					detector = DetectRescue
				case slices.Contains(basis, id):
					detector = DetectVerifier
				case named[id] || named[ShortID(id)]:
					detector = DetectPerson
				}
				if detector != "" && !missed[id] {
					missed[id] = true
					records.Misses = append(records.Misses, Miss{Capture: entryID, Hidden: id, Rule: rule, Detector: detector, Judge: judge, At: at})
				}
			}
		}
		if held.cut > 0 {
			for _, id := range answered {
				if score, ok := held.scores[id]; ok {
					tally.Margins = append(tally.Margins, ScoreValue(score)-ScoreValue(held.cut))
				}
			}
		}
		if len(tally.Rules) > 0 || len(tally.Margins) > 0 {
			records.Tallies = append(records.Tallies, tally)
		}
	}
	return records
}

// hidden returns the candidates rule hid in the reading, highest score
// first, the closest to the line leading.
func (h *readingScores) hidden(rule HidingRule) []string {
	set := h.setAside
	if rule == RuleFarFloor {
		set = h.below
	}
	ids := slices.Sorted(maps.Keys(set))
	slices.SortStableFunc(ids, func(a, b string) int { return cmp.Compare(h.scores[b], h.scores[a]) })
	return ids
}

// WouldSetAside counts the scored candidates the threshold would set aside
// at its full step: below thresholds' cut and not below the far floor.
func WouldSetAside(candidates []GatherCandidate, scores map[string]uint8, belowFloor map[string]bool, thresholds RelevanceThresholds) int {
	if !thresholds.Valid {
		return 0
	}
	would := 0
	for _, candidate := range candidates {
		score, ok := scores[candidate.Entry.ID]
		if ok && !belowFloor[candidate.Entry.ID] && ScoreValue(score) < thresholds.SetAside {
			would++
		}
	}
	return would
}

// MarginWarn is the band above the threshold's cut whose share of
// references warns that the cut sits too high.
const MarginWarn = 0.05

// HidingReport is what `sdd grounding status` shows of the hiding rules for
// one judge: each rule's standing and record, the scan's detection rate on
// planted misses, and the margins of references above the cut.
type HidingReport struct {
	Judge     string
	Revision  string
	GraphSize int
	Cut       float64
	Replays   int
	Rules     []RuleReport
	// Planted counts the planted misses among the valid replays, Detection
	// the share the scan after writing caught.
	Planted   int
	Detection float64
	Margins   MarginReport
}

// RuleReport is one rule's standing and what the tallies of the judge's
// checks record of it: the checks it took part in, the entries it hid and
// those it would have hidden at the full step, and its misses by detector.
type RuleReport struct {
	Rule   HidingRule
	State  RuleState
	Checks int
	Hid    int
	Would  int
	Misses map[MissDetector]int
}

// MarginReport counts the margins of references recorded: Below the cut,
// and Close within MarginWarn above it.
type MarginReport struct {
	Count, Below, Close int
}

// NewHidingReport folds records into the report of judge at revision on a
// graph of graphSize live entries, the threshold calibrated as thresholds,
// a rule its replays do not prove taking its step from team (ApplyTeamProof).
func NewHidingReport(records ProofRecords, judge, revision string, graphSize int, thresholds RelevanceThresholds, team *TeamProof) HidingReport {
	valid := ValidReplays(records.Replays, judge, revision, graphSize)
	report := HidingReport{Judge: judge, Revision: revision, GraphSize: graphSize, Replays: len(valid)}
	if thresholds.Valid {
		report.Cut = thresholds.SetAside
	}
	report.Detection, report.Planted = DetectionRate(valid)
	for _, rule := range HidingRules() {
		rr := RuleReport{Rule: rule, State: ApplyTeamProof(HidingState(rule, records, judge, revision, graphSize, thresholds), team, records, judge, revision, graphSize), Misses: map[MissDetector]int{}}
		for _, tally := range records.Tallies {
			if CanonicalJudge(tally.Judge) != CanonicalJudge(judge) {
				continue
			}
			for _, mark := range tally.Rules {
				if mark.Rule == rule {
					rr.Checks++
					rr.Hid += len(mark.Hidden)
					rr.Would += mark.Would
				}
			}
		}
		for _, miss := range records.Misses {
			if miss.Rule == rule && CanonicalJudge(miss.Judge) == CanonicalJudge(judge) {
				rr.Misses[miss.Detector]++
			}
		}
		report.Rules = append(report.Rules, rr)
	}
	for _, tally := range records.Tallies {
		if CanonicalJudge(tally.Judge) != CanonicalJudge(judge) {
			continue
		}
		for _, margin := range tally.Margins {
			report.Margins.Count++
			switch {
			case margin < 0:
				report.Margins.Below++
			case margin <= MarginWarn:
				report.Margins.Close++
			}
		}
	}
	return report
}

var (
	namedFullID  = regexp.MustCompile(`\b\d{8}-\d{6}-[a-z]-[a-z]{3}-[a-z0-9]+\b`)
	namedShortID = regexp.MustCompile(`\b[a-z]-[a-z]{3}-[a-z0-9]+\b`)
)

// NamedEntries returns the entry IDs words name, in full or in the short
// type-layer-suffix form, as a set holding each form found.
func NamedEntries(words []string) map[string]bool {
	named := map[string]bool{}
	for _, text := range words {
		for _, id := range namedFullID.FindAllString(text, -1) {
			named[id] = true
		}
		for _, id := range namedShortID.FindAllString(namedFullID.ReplaceAllString(text, " "), -1) {
			named[id] = true
		}
	}
	return named
}
