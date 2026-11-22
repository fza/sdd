package model

import "slices"

// VerifierDraft is what the deep verifier's reading order reads of a draft:
// the entries it declares as refs, supersedes, closes and approves.
type VerifierDraft struct {
	Targets []string
}

// Why the deep verifier's reading marks an entry: one closing or superseding
// a target, one a target closes or supersedes, or one the shared order places
// as likely bearing.
const (
	VerifierClosesTarget     = "closes a target"
	VerifierSupersedesTarget = "supersedes a target"
	VerifierTargetCloses     = "a target closes it"
	VerifierTargetSupersedes = "a target supersedes it"
	VerifierLikelyBearing    = "likely bearing"
)

// VerifierReading is the grounding reading the deep verifier's order is
// drawn from: the entries listed, read in full and given a verdict, the
// ranking judge's scores against the draft, its calibration, the seed the
// audit sample is drawn by, and the similarity to the draft of entries the
// judge left unscored. Widened doubles the lines in doubt, for a judge whose
// ranking health is weak (RankingHealth).
type VerifierReading struct {
	Listed, Full, Verdicts []string
	// Reads are the capture's searches run again against the final draft,
	// whose merged order places the candidates (OrderGatherCandidates).
	Reads      []GroundingRead
	Scores     map[string]uint8
	Thresholds RelevanceThresholds
	Seed       string
	Similarity map[string]float64
	Widened    bool
}

// VerifierPlacement is the deep verifier's start: the declared targets in
// full and every other entry as lines of the shared placement (PlaceLines).
type VerifierPlacement struct {
	Targets []string
	Lines   Lines
}

// Candidates returns every entry the placement lists beside the targets:
// lines, title lines and counted ones.
func (p VerifierPlacement) Candidates() []string {
	var ids []string
	for _, line := range slices.Concat(p.Lines.Bearing, p.Lines.Doubt, p.Lines.Titles) {
		ids = append(ids, line.Entry.ID)
	}
	return append(ids, p.Lines.Counted...)
}

// PlaceVerifierReading places the deep verifier's start. Only the declared
// targets come in full. The candidates are the listed entries, those the
// drafter read in full, those the capture's searches found again against
// the final draft and every lifecycle neighbour of a target; they stand in
// the merged order of those searches (OrderGatherCandidates), the ones no
// search found by falling similarity to the draft, done entries last among
// the unscored. They pass through the placement a graph check uses
// (PlaceLines), with the lifecycle neighbours marked by their reason and the
// entries one reference from a target carrying the hop. With no candidate
// scored the placement is judged by the verifier: the first unscored carry
// the mark. An entry with a drafter's verdict is a line with its first
// summary sentence, never a title line or a count (20261122-101625-d-tac-r0s).
func PlaceVerifierReading(g *Graph, draft VerifierDraft, reading VerifierReading) VerifierPlacement {
	placement := VerifierPlacement{Targets: verifierTargets(g, draft)}
	lifecycle := verifierLifecycle(g, placement.Targets)
	candidates := VerifierCandidates(g, draft, reading)
	scored := false
	for _, candidate := range candidates {
		_, ok := reading.Scores[candidate.Entry.ID]
		scored = scored || ok
	}
	hops := RefHops(g, placement.Targets)
	placement.Lines = PlaceLines(LineInput{
		Candidates: candidates, Scores: reading.Scores, Thresholds: reading.Thresholds, Widened: reading.Widened,
		SelfJudged: !scored, Hops: hops, Marked: lifecycle.reasons, Seed: reading.Seed,
	})
	for i := range placement.Lines.Bearing {
		if placement.Lines.Bearing[i].Reason == "" {
			placement.Lines.Bearing[i].Reason = VerifierLikelyBearing
		}
	}
	verdict := func(id string) bool { return slices.Contains(reading.Verdicts, id) }
	for i, line := range placement.Lines.Doubt {
		if verdict(line.Entry.ID) {
			placement.Lines.Doubt[i].Summary = true
		}
	}
	placement.Lines.Titles = slices.DeleteFunc(placement.Lines.Titles, func(line Line) bool {
		if !verdict(line.Entry.ID) {
			return false
		}
		line.Summary = true
		placement.Lines.Doubt = append(placement.Lines.Doubt, line)
		return true
	})
	placement.Lines.Counted = slices.DeleteFunc(placement.Lines.Counted, func(id string) bool {
		if !verdict(id) {
			return false
		}
		line := Line{Entry: g.ByID[id], Summary: true, Hop: hops[id]}
		if score, ok := reading.Scores[id]; ok {
			value := ScoreValue(score)
			line.Score = &value
		}
		placement.Lines.Doubt = append(placement.Lines.Doubt, line)
		return true
	})
	return placement
}

// VerifierCandidates returns the deep verifier's candidates in the merged
// order PlaceVerifierReading places them by.
func VerifierCandidates(g *Graph, draft VerifierDraft, reading VerifierReading) []GatherCandidate {
	targets := verifierTargets(g, draft)
	seen := map[string]bool{}
	for _, id := range targets {
		seen[id] = true
	}
	var found []string
	for _, read := range reading.Reads {
		found = append(found, read.IDs...)
	}
	var candidates []GatherCandidate
	for _, id := range slices.Concat(reading.Listed, reading.Full, found, verifierLifecycle(g, targets).order) {
		if g.ByID[id] != nil && !seen[id] {
			seen[id] = true
			candidates = append(candidates, GatherCandidate{Entry: g.ByID[id]})
		}
	}
	return OrderGatherCandidates(g, candidates, reading.Reads, reading.Similarity, targets)
}

func verifierTargets(g *Graph, draft VerifierDraft) []string {
	var targets []string
	for _, id := range draft.Targets {
		if g.ByID[id] != nil && !slices.Contains(targets, id) {
			targets = append(targets, id)
		}
	}
	return targets
}

func rankLast(last bool) int {
	if last {
		return 1
	}
	return 0
}

// lifecycleMarks are a target's lifecycle neighbours with the reason each is
// marked, in the order found.
type lifecycleMarks struct {
	order   []string
	reasons map[string]string
}

func verifierLifecycle(g *Graph, targets []string) lifecycleMarks {
	marks := lifecycleMarks{reasons: map[string]string{}}
	add := func(id, reason string) {
		if slices.Contains(targets, id) || g.ByID[id] == nil {
			return
		}
		if _, ok := marks.reasons[id]; !ok {
			marks.order = append(marks.order, id)
			marks.reasons[id] = reason
		}
	}
	for _, id := range targets {
		for _, other := range g.ClosedBy[id] {
			add(other, VerifierClosesTarget)
		}
		for _, other := range g.SupersededBy[id] {
			add(other, VerifierSupersedesTarget)
		}
		target := g.ByID[id]
		for _, other := range target.Closes {
			add(other, VerifierTargetCloses)
		}
		for _, other := range target.Supersedes {
			add(other, VerifierTargetSupersedes)
		}
	}
	return marks
}

// VerifierUnscored returns the candidates of the placement that hold no
// score in reading and no mark of their own: their score decides their
// class.
func VerifierUnscored(placement VerifierPlacement, reading VerifierReading) []string {
	own := map[string]bool{}
	for _, line := range placement.Lines.Bearing {
		own[line.Entry.ID] = line.Reason != VerifierLikelyBearing
	}
	return slices.DeleteFunc(placement.Candidates(), func(id string) bool {
		_, scored := reading.Scores[id]
		return scored || own[id]
	})
}

// RankingSources counts the entries a reading's rank placed by what ranked
// each: the judge's score, the merged order of the searches that found it,
// similarity to the draft, or listed order.
type RankingSources struct {
	Judged, Searched, Similar, Listed int
}

// Kind names the source: judge, searches or similarity when one ranked every
// entry, listed order when none ranked any, mixed otherwise; empty when the
// rank placed no entry.
func (s RankingSources) Kind() string {
	switch {
	case s.Judged+s.Searched+s.Similar+s.Listed == 0:
		return ""
	case s.Judged > 0 && s.Searched+s.Similar+s.Listed == 0:
		return "judge"
	case s.Searched > 0 && s.Judged+s.Similar+s.Listed == 0:
		return "searches"
	case s.Similar > 0 && s.Judged+s.Searched+s.Listed == 0:
		return "similarity"
	case s.Judged+s.Searched+s.Similar == 0:
		return "listed order"
	}
	return "mixed"
}

// VerifierRankingSources counts the candidates of the placement by what
// ranked them in reading.
func VerifierRankingSources(placement VerifierPlacement, reading VerifierReading) RankingSources {
	var sources RankingSources
	searched := map[string]bool{}
	for _, read := range reading.Reads {
		if read.Mode == GroundingSemantic || read.Mode == GroundingKeyword {
			for _, id := range read.IDs {
				searched[id] = true
			}
		}
	}
	for _, id := range placement.Candidates() {
		_, scored := reading.Scores[id]
		_, near := reading.Similarity[id]
		switch {
		case scored:
			sources.Judged++
		case searched[id]:
			sources.Searched++
		case near:
			sources.Similar++
		default:
			sources.Listed++
		}
	}
	return sources
}
