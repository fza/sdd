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

// The cut points of the deep verifier's reading. Of the ranked entries the
// first DeepTopRanked are served in full; until the prefilter is calibrated
// the next DeepSummaryRanked are listed by title and first summary sentence
// and the rest by title alone, after it the set-aside threshold draws that
// line. At most DeepHopListed entries one ref from a target and
// DeepTitleListed entries by title are listed, by rank; the rest are counted.
const (
	DeepTopRanked     = 5
	DeepSummaryRanked = 100
	DeepHopListed     = 50
	DeepTitleListed   = 300
)

// VerifierRole is why the deep verifier's reading holds an entry.
type VerifierRole string

const (
	VerifierRoleTarget VerifierRole = "declared target"
	VerifierRoleRead   VerifierRole = "read in full"
	// VerifierRoleLifecycle is an entry closing or superseding a target, or
	// one a target closes or supersedes.
	VerifierRoleLifecycle VerifierRole = "lifecycle neighbour of a target"
	VerifierRoleHop       VerifierRole = "one ref from a target"
	VerifierRoleVerdict   VerifierRole = "given a verdict"
	VerifierRoleRanked    VerifierRole = "top-ranked"
)

// VerifierPlace is how the deep verifier's reading serves an entry.
type VerifierPlace int

const (
	// VerifierPlaceFull serves the entry's block with its body.
	VerifierPlaceFull VerifierPlace = iota
	// VerifierPlaceBrief serves the entry's block with its brief.
	VerifierPlaceBrief
	// VerifierPlaceSummary lists the entry by title and first summary
	// sentence.
	VerifierPlaceSummary
	// VerifierPlaceTitle lists the entry by title alone.
	VerifierPlaceTitle
	// VerifierPlaceCounted only counts the entry, past a listing ceiling.
	VerifierPlaceCounted
)

// VerifierSlot is one entry of the deep verifier's reading, in reading
// order. Hidden marks an entry the calibrated prefilter scored below its
// set-aside threshold.
type VerifierSlot struct {
	ID     string
	Role   VerifierRole
	Place  VerifierPlace
	Hidden bool
}

// VerifierReading is the grounding reading the deep verifier's order is
// drawn from: the entries listed, read in full and given a verdict, the
// ranking judge's scores against the draft, its calibration, the seed the
// audit sample is drawn by, and the similarity to the draft of entries the
// judge left unscored. Widened doubles DeepTopRanked and DeepSummaryRanked,
// for a judge whose ranking health is weak (RankingHealth).
type VerifierReading struct {
	Listed, Full, Verdicts []string
	Scores                 map[string]uint8
	Thresholds             RelevanceThresholds
	Seed                   string
	Similarity             map[string]float64
	Widened                bool
}

// tiers returns how many ranked entries the reading serves in full and how
// many it lists with their summary after them.
func (r VerifierReading) tiers() (top, summary int) {
	if r.Widened {
		return 2 * DeepTopRanked, 2 * DeepSummaryRanked
	}
	return DeepTopRanked, DeepSummaryRanked
}

// PlaceVerifierReading orders the deep verifier's reading and places each
// entry. Served in full, however many: the declared targets, the entries
// the drafter read in full, and every listed lifecycle neighbour of a target
// (verifierNeighbours). Then the entries given a verdict, as briefs, and the
// listed entries one ref from a target by title and first summary sentence,
// up to DeepHopListed by rank. Rank is falling score, a done entry last among
// equal scores; unscored entries follow the scored, done ones last, the rest
// by falling similarity, then in listed order. The rest of the
// listed entries go by rank: the first DeepTopRanked that are not done, and
// scored unless every ranked entry fits them, are served in full, the next ones up to DeepSummaryRanked listed by title and
// first summary sentence, the rest by title, up to DeepTitleListed. Once
// calibrated, every ranked entry past the top is listed with its summary
// sentence, but one scoring below the set-aside threshold is hidden, listed
// by title after every other; the AuditSample of them ranks blind among the
// others instead. What passes a ceiling is counted.
func PlaceVerifierReading(g *Graph, draft VerifierDraft, reading VerifierReading) []VerifierSlot {
	var slots []VerifierSlot
	placed := map[string]bool{}
	place := func(id string, role VerifierRole, at VerifierPlace) {
		if placed[id] || g.ByID[id] == nil {
			return
		}
		placed[id] = true
		slots = append(slots, VerifierSlot{ID: id, Role: role, Place: at})
	}
	for _, id := range draft.Targets {
		place(id, VerifierRoleTarget, VerifierPlaceFull)
	}
	for _, id := range reading.Full {
		place(id, VerifierRoleRead, VerifierPlaceFull)
	}
	lifecycle, refs := verifierNeighbours(g, draft.Targets)
	for _, id := range reading.Listed {
		if lifecycle[id] {
			place(id, VerifierRoleLifecycle, VerifierPlaceFull)
		}
	}
	for _, id := range reading.Listed {
		if slices.Contains(reading.Verdicts, id) {
			place(id, VerifierRoleVerdict, VerifierPlaceBrief)
		}
	}
	done := func(id string) bool { return g.ByID[id].Kind == KindDone }
	byRank := func(a, b string) int {
		scoreA, scoredA := reading.Scores[a]
		scoreB, scoredB := reading.Scores[b]
		if c := cmp.Compare(rankLast(!scoredA), rankLast(!scoredB)); c != 0 {
			return c
		}
		if scoredA && scoreA != scoreB {
			return cmp.Compare(scoreB, scoreA)
		}
		if c := cmp.Compare(rankLast(done(a)), rankLast(done(b))); c != 0 {
			return c
		}
		if scoredA {
			return 0
		}
		similarA, nearA := reading.Similarity[a]
		similarB, nearB := reading.Similarity[b]
		if c := cmp.Compare(rankLast(!nearA), rankLast(!nearB)); c != 0 {
			return c
		}
		return cmp.Compare(similarB, similarA)
	}
	var hops []string
	for _, id := range reading.Listed {
		if refs[id] && !placed[id] && g.ByID[id] != nil && !slices.Contains(hops, id) {
			hops = append(hops, id)
		}
	}
	slices.SortStableFunc(hops, byRank)
	for i, id := range hops {
		at := VerifierPlaceSummary
		if i >= DeepHopListed {
			at = VerifierPlaceCounted
		}
		place(id, VerifierRoleHop, at)
	}
	titles := 0
	title := func() VerifierPlace {
		titles++
		if titles > DeepTitleListed {
			return VerifierPlaceCounted
		}
		return VerifierPlaceTitle
	}
	var ranked, below []string
	queued := map[string]bool{}
	for _, id := range reading.Listed {
		if placed[id] || queued[id] || g.ByID[id] == nil {
			continue
		}
		queued[id] = true
		score, scored := reading.Scores[id]
		if reading.Thresholds.Valid && scored && ScoreValue(score) < reading.Thresholds.SetAside {
			below = append(below, id)
		} else {
			ranked = append(ranked, id)
		}
	}
	audit, _ := AuditSample(below, reading.Seed)
	slices.SortStableFunc(ranked, byRank)
	for _, id := range audit {
		ranked = slices.Insert(ranked, blindPosition(reading.Seed, id, len(ranked)), id)
	}
	top, summary := reading.tiers()
	allFit := len(ranked) <= top
	for i, id := range ranked {
		_, scored := reading.Scores[id]
		switch {
		case i < top && !done(id) && (scored || allFit):
			place(id, VerifierRoleRanked, VerifierPlaceFull)
		case reading.Thresholds.Valid || i < top+summary:
			place(id, VerifierRoleRanked, VerifierPlaceSummary)
		default:
			place(id, VerifierRoleRanked, title())
		}
	}
	for _, id := range below {
		if !slices.Contains(audit, id) {
			place(id, VerifierRoleRanked, title())
			slots[len(slots)-1].Hidden = true
		}
	}
	return slots
}

func rankLast(last bool) int {
	if last {
		return 1
	}
	return 0
}

// verifierNeighbours returns the entries one hop from a target: lifecycle
// closes or supersedes a target or is closed or superseded by one; refs
// references a target or is referenced by one.
func verifierNeighbours(g *Graph, targets []string) (lifecycle, refs map[string]bool) {
	lifecycle, refs = map[string]bool{}, map[string]bool{}
	for _, id := range targets {
		if target := g.ByID[id]; target != nil {
			for _, ref := range target.Refs {
				refs[ref.ID] = true
			}
			for _, other := range slices.Concat(target.Closes, target.Supersedes) {
				lifecycle[other] = true
			}
		}
		for _, other := range slices.Concat(g.ClosedBy[id], g.SupersededBy[id]) {
			lifecycle[other] = true
		}
		for _, other := range g.RefsTo[id] {
			refs[other] = true
		}
	}
	return lifecycle, refs
}

// VerifierUnscored returns the entries of slots that hold no score in
// reading where their rank decides how they are served: the ranked ones once
// calibrated, since the threshold hides by score, else only when more are
// ranked than the top serves in full; the ref neighbours when more are
// listed than DeepHopListed.
func VerifierUnscored(slots []VerifierSlot, reading VerifierReading) []string {
	var ranked, hops []string
	for _, slot := range slots {
		switch slot.Role {
		case VerifierRoleRanked:
			ranked = append(ranked, slot.ID)
		case VerifierRoleHop:
			hops = append(hops, slot.ID)
		}
	}
	top, _ := reading.tiers()
	var decides []string
	if reading.Thresholds.Valid || len(ranked) > top {
		decides = ranked
	}
	if len(hops) > DeepHopListed {
		decides = append(decides, hops...)
	}
	return slices.DeleteFunc(decides, func(id string) bool {
		_, scored := reading.Scores[id]
		return scored
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

// VerifierRankingSources counts the ranked entries and ref neighbours of
// slots by what ranked them in reading.
func VerifierRankingSources(slots []VerifierSlot, reading VerifierReading) RankingSources {
	var sources RankingSources
	for _, slot := range slots {
		if slot.Role != VerifierRoleRanked && slot.Role != VerifierRoleHop {
			continue
		}
		if _, scored := reading.Scores[slot.ID]; scored {
			sources.Judged++
		} else if _, near := reading.Similarity[slot.ID]; near {
			sources.Similar++
		} else {
			sources.Listed++
		}
	}
	return sources
}
