package model

import (
	"cmp"
	"slices"
)

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
	Scores                 map[string]uint8
	Thresholds             RelevanceThresholds
	Seed                   string
	Similarity             map[string]float64
	Widened                bool
}

// VerifierPlacement is the deep verifier's start: the declared targets in
// full, the entries with a drafter's verdict that no mark holds as briefs,
// and every other entry as lines of the shared placement (PlaceLines).
type VerifierPlacement struct {
	Targets []string
	Briefs  []string
	Lines   Lines
}

// Candidates returns every entry the placement lists beside the targets:
// briefs, lines, title lines and counted ones.
func (p VerifierPlacement) Candidates() []string {
	ids := slices.Clone(p.Briefs)
	for _, line := range slices.Concat(p.Lines.Bearing, p.Lines.Doubt, p.Lines.Titles) {
		ids = append(ids, line.Entry.ID)
	}
	return append(ids, p.Lines.Counted...)
}

// PlaceVerifierReading places the deep verifier's start. Only the declared
// targets come in full. The candidates are the listed entries, those the
// drafter read in full and every lifecycle neighbour of a target; they pass
// through the placement a graph check uses (PlaceLines), with the lifecycle
// neighbours marked by their reason and the entries one reference from a
// target carrying the hop. Unscored candidates stand by falling similarity
// to the draft, then in listed order, done entries after all others. With no
// candidate scored the placement is judged by the verifier: the first
// unscored carry the mark. An entry with a drafter's verdict the placement
// does not mark is a brief.
func PlaceVerifierReading(g *Graph, draft VerifierDraft, reading VerifierReading) VerifierPlacement {
	var placement VerifierPlacement
	seen := map[string]bool{}
	for _, id := range draft.Targets {
		if g.ByID[id] != nil && !seen[id] {
			seen[id] = true
			placement.Targets = append(placement.Targets, id)
		}
	}
	lifecycle := verifierLifecycle(g, placement.Targets)
	var order []string
	for _, id := range slices.Concat(reading.Listed, reading.Full, lifecycle.order) {
		if g.ByID[id] != nil && !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
	}
	done := func(id string) bool { return g.ByID[id].Kind == KindDone }
	slices.SortStableFunc(order, func(a, b string) int {
		if c := cmp.Compare(rankLast(done(a)), rankLast(done(b))); c != 0 {
			return c
		}
		similarA, nearA := reading.Similarity[a]
		similarB, nearB := reading.Similarity[b]
		if c := cmp.Compare(rankLast(!nearA), rankLast(!nearB)); c != 0 {
			return c
		}
		return cmp.Compare(similarB, similarA)
	})
	scored := false
	candidates := make([]GatherCandidate, 0, len(order))
	for _, id := range order {
		candidates = append(candidates, GatherCandidate{Entry: g.ByID[id]})
		_, ok := reading.Scores[id]
		scored = scored || ok
	}
	placement.Lines = PlaceLines(LineInput{
		Candidates: candidates, Scores: reading.Scores, Thresholds: reading.Thresholds, Widened: reading.Widened,
		SelfJudged: !scored, Hops: RefHops(g, placement.Targets), Marked: lifecycle.reasons, Seed: reading.Seed,
	})
	for i := range placement.Lines.Bearing {
		if placement.Lines.Bearing[i].Reason == "" {
			placement.Lines.Bearing[i].Reason = VerifierLikelyBearing
		}
	}
	verdict := func(id string) bool { return slices.Contains(reading.Verdicts, id) }
	briefs := func(lines []Line) []Line {
		return slices.DeleteFunc(lines, func(line Line) bool {
			if verdict(line.Entry.ID) {
				placement.Briefs = append(placement.Briefs, line.Entry.ID)
				return true
			}
			return false
		})
	}
	placement.Lines.Doubt = briefs(placement.Lines.Doubt)
	placement.Lines.Titles = briefs(placement.Lines.Titles)
	placement.Lines.Counted = slices.DeleteFunc(placement.Lines.Counted, func(id string) bool {
		if verdict(id) {
			placement.Briefs = append(placement.Briefs, id)
			return true
		}
		return false
	})
	return placement
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
// each: the judge's score, similarity to the draft, or listed order.
type RankingSources struct {
	Judged, Similar, Listed int
}

// Kind names the source: judge or similarity when one ranked every entry,
// listed order when neither ranked any, mixed otherwise; empty when the rank
// placed no entry.
func (s RankingSources) Kind() string {
	switch {
	case s.Judged+s.Similar+s.Listed == 0:
		return ""
	case s.Judged > 0 && s.Similar == 0 && s.Listed == 0:
		return "judge"
	case s.Similar > 0 && s.Judged == 0 && s.Listed == 0:
		return "similarity"
	case s.Judged == 0 && s.Similar == 0:
		return "listed order"
	}
	return "mixed"
}

// VerifierRankingSources counts the candidates of the placement by what
// ranked them in reading.
func VerifierRankingSources(placement VerifierPlacement, reading VerifierReading) RankingSources {
	var sources RankingSources
	for _, id := range placement.Candidates() {
		if _, scored := reading.Scores[id]; scored {
			sources.Judged++
		} else if _, near := reading.Similarity[id]; near {
			sources.Similar++
		} else {
			sources.Listed++
		}
	}
	return sources
}
