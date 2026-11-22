package model

import (
	"cmp"
	"slices"
)

// The limits a graph check and the deep verifier's reading share
// (20261121-223938-d-tac-rqi): every line marked as likely bearing shows; of
// the entries in doubt DoubtLines show as lines, doubled for a weak judge,
// the first DoubtSummaryLines with their first summary sentence, then up to
// TitleLines as title lines, and the rest are counted. Where the agent judges
// the check itself, the first UnjudgedSummaryLines unscored lines carry their
// summary sentence.
const (
	DoubtLines           = 100
	DoubtSummaryLines    = 30
	UnjudgedSummaryLines = 50
	TitleLines           = 300
)

// The marks without learned thresholds (20261121-233807-d-tac-4iw): a judge
// that scored marks at most FallbackMarks lines, each scored at least
// FallbackMarkScore; a check without scores marks its first FallbackMarks
// unscored lines.
const (
	FallbackMarks     = 5
	FallbackMarkScore = 0.5
)

// Line is one entry a reading lists. Marked lines are likely bearing and owe
// an answer; Reason says why a caller marked one. A nil Score is not judged.
// Summary adds the first summary sentence; Hop is one reference step from an
// entry the draft references; Blind is an audit sample of a set-aside entry,
// shown unscored.
type Line struct {
	Entry   *Entry
	Score   *float64
	Marked  bool
	Reason  string
	Summary bool
	Hop     bool
	Blind   bool
}

// LineInput is what PlaceLines places. Candidates stand in the order they go
// to the judge, which places the unscored ones. Marked names candidates the
// caller marks whatever their score, with the reason. SelfJudged is a check
// whose judge failed, is off or fell short, so the agent judges it. Seed draws
// the blind audit sample of the set-aside entries; empty draws none.
type LineInput struct {
	Candidates []GatherCandidate
	Scores     map[string]uint8
	BelowFloor map[string]bool
	Thresholds RelevanceThresholds
	Widened    bool
	SelfJudged bool
	Hops       map[string]bool
	Marked     map[string]string
	Seed       string
}

// Lines is a placed reading: the lines marked as likely bearing, the lines in
// doubt, the title lines and the counted IDs past them, and the set-aside
// groups. Audit names the set-aside entries shown blind among the lines in
// doubt; Judged counts the candidates holding a score.
type Lines struct {
	Bearing  []Line
	Doubt    []Line
	Titles   []Line
	Counted  []string
	SetAside []GatherSetAside
	Audit    []string
	Judged   int
}

// Shown returns the IDs of the bearing and doubt lines: what the reading
// showed, title lines and counts left out.
func (l Lines) Shown() []string {
	var ids []string
	for _, line := range slices.Concat(l.Bearing, l.Doubt) {
		ids = append(ids, line.Entry.ID)
	}
	return ids
}

// MarkedIDs returns the IDs of the bearing lines.
func (l Lines) MarkedIDs() []string {
	var ids []string
	for _, line := range l.Bearing {
		ids = append(ids, line.Entry.ID)
	}
	return ids
}

// PlaceLines places candidates into the three classes under one set of
// limits. A candidate the caller marks is likely bearing. A candidate below
// the far floor is set aside; with valid thresholds so is a score below
// SetAside, and a score at or above Relevant is likely bearing. Without valid
// thresholds the best FallbackMarks scores at or above FallbackMarkScore are
// likely bearing; in a check the agent judges itself, the first FallbackMarks
// unscored candidates are. Every other candidate is in doubt, scored ones
// first by falling score, then the unscored in candidate order, and falls
// under the limits of DoubtLines and TitleLines.
func PlaceLines(in LineInput) Lines {
	var placed Lines
	var scored, unscored []Line
	var setAside, floored []string
	byID := map[string]*Entry{}
	for _, candidate := range in.Candidates {
		id := candidate.Entry.ID
		byID[id] = candidate.Entry
		line := Line{Entry: candidate.Entry, Hop: in.Hops[id]}
		score, ok := in.Scores[id]
		if ok {
			value := ScoreValue(score)
			line.Score = &value
			placed.Judged++
		}
		reason, marked := in.Marked[id]
		switch {
		case marked:
			line.Marked, line.Reason = true, reason
			placed.Bearing = append(placed.Bearing, line)
		case in.BelowFloor[id]:
			floored = append(floored, id)
		case ok && in.Thresholds.Valid && *line.Score < in.Thresholds.SetAside:
			setAside = append(setAside, id)
		case ok && in.Thresholds.Valid && *line.Score >= in.Thresholds.Relevant:
			line.Marked = true
			placed.Bearing = append(placed.Bearing, line)
		case ok:
			scored = append(scored, line)
		default:
			unscored = append(unscored, line)
		}
	}
	slices.SortStableFunc(scored, byFallingScore)
	if !in.Thresholds.Valid {
		n := 0
		for n < len(scored) && n < FallbackMarks && *scored[n].Score >= FallbackMarkScore {
			scored[n].Marked = true
			n++
		}
		placed.Bearing = append(placed.Bearing, scored[:n]...)
		scored = scored[n:]
	}
	unjudged := map[string]int{}
	for i, line := range unscored {
		unjudged[line.Entry.ID] = i
	}
	if in.SelfJudged {
		n := min(FallbackMarks, len(unscored))
		for i := range n {
			unscored[i].Marked = true
		}
		placed.Bearing = append(placed.Bearing, unscored[:n]...)
		unscored = unscored[n:]
	}
	slices.SortStableFunc(placed.Bearing, byFallingScore)

	doubt := slices.Concat(scored, unscored)
	if in.Seed != "" {
		var kept []string
		placed.Audit, kept = AuditSample(slices.Concat(setAside, floored), in.Seed)
		setAside = slices.DeleteFunc(setAside, func(id string) bool { return !slices.Contains(kept, id) })
		floored = slices.DeleteFunc(floored, func(id string) bool { return !slices.Contains(kept, id) })
		for _, id := range placed.Audit {
			doubt = slices.Insert(doubt, blindPosition(in.Seed, id, len(doubt)), Line{Entry: byID[id], Hop: in.Hops[id], Blind: true})
		}
	}
	limit := DoubtLines
	if in.Widened {
		limit *= 2
	}
	for i, line := range doubt {
		switch {
		case i < limit:
			position, isUnjudged := unjudged[line.Entry.ID]
			line.Summary = i < DoubtSummaryLines || in.SelfJudged && isUnjudged && !line.Blind && position < UnjudgedSummaryLines
			placed.Doubt = append(placed.Doubt, line)
		case len(placed.Titles) < TitleLines:
			placed.Titles = append(placed.Titles, line)
		default:
			placed.Counted = append(placed.Counted, line.Entry.ID)
		}
	}
	slices.Sort(setAside)
	slices.Sort(floored)
	if len(setAside) > 0 {
		placed.SetAside = append(placed.SetAside, GatherSetAside{Reason: SetAsideFiltered, IDs: setAside})
	}
	if len(floored) > 0 {
		placed.SetAside = append(placed.SetAside, GatherSetAside{Reason: SetAsideFloor, IDs: floored})
	}
	return placed
}

func byFallingScore(a, b Line) int {
	switch {
	case a.Score != nil && b.Score == nil:
		return -1
	case a.Score == nil && b.Score != nil:
		return 1
	case a.Score != nil && b.Score != nil:
		return cmp.Compare(*b.Score, *a.Score)
	}
	return 0
}

// RefHops returns the entries one reference step from targets: those a
// target references and those referencing a target.
func RefHops(g *Graph, targets []string) map[string]bool {
	refs := map[string]bool{}
	for _, id := range targets {
		if target := g.ByID[id]; target != nil {
			for _, ref := range target.Refs {
				refs[ref.ID] = true
			}
		}
		for _, other := range g.RefsTo[id] {
			refs[other] = true
		}
	}
	return refs
}

// NewerRefiner returns the newest live entry that refines id and is newer
// than it, empty when none does.
func NewerRefiner(g *Graph, id string) string {
	e := g.ByID[id]
	if e == nil {
		return ""
	}
	newest := ""
	for _, other := range g.RefsTo[id] {
		refiner := g.ByID[other]
		if refiner == nil || !refiner.Time.After(e.Time) || len(g.SupersededBy[other]) > 0 || len(g.ClosedBy[other]) > 0 {
			continue
		}
		refines := slices.ContainsFunc(refiner.Refs, func(ref Ref) bool { return ref.ID == id && ref.Kind == RefKindRefines })
		if refines && (newest == "" || refiner.Time.After(g.ByID[newest].Time)) {
			newest = other
		}
	}
	return newest
}
