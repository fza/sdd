package model

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"slices"
)

// The far floor: every judge call of a reading also scores FarFloorSize
// entries far from the draft, and a candidate the judge rates no higher than
// the line their scores set in its own call (FarForm) is set aside. An entry is far when it shares
// no topic area, the first component of a topic path, with the draft and
// lies more than FarFloorHops reference hops from every entry the draft
// references. It needs no label, so it holds for a judge without thresholds.
const (
	FarFloorSize = 3
	FarFloorHops = 2
)

// FarDraft is what the far floor reads of a draft: the hash of its relevance
// text (RelevanceSubjectHash), its topics and the entries it references.
type FarDraft struct {
	Subject    string
	Topics     []string
	References []string
}

// FarForm is the form of the far floor's line: careful, the lowest of a
// call's far scores, or median, their median. The floor starts careful and
// takes the median form only at the full step (FloorStep).
type FarForm uint8

const (
	FarCareful FarForm = iota
	FarMedian
)

// FarFloor is the far floor of one reading: the far entries its judge calls
// carry, the form of its line, and the neighbourhood of the draft's
// references that no score puts below it.
type FarFloor struct {
	Far  []*Entry
	Form FarForm
	near map[string]bool
}

// Line is the line of a call whose far scores have lowest as their lowest
// and median as their median, in the floor's form.
func (f *FarFloor) Line(lowest, median float64) float64 {
	if f != nil && f.Form == FarMedian {
		return median
	}
	return lowest
}

// NewFarFloor picks a draft's far entries among population, leaving out
// listed, the entries the reading lists or the draft answers, and the
// entries the binary ships, which are no part of the project's graph: the
// first FarFloorSize by a hash of the draft's subject hash and the entry
// ID, so every reading of one draft picks alike. Nil for a draft with
// neither a topic nor a reference, which leaves nothing to be far from, and
// where fewer than FarFloorSize entries are far.
func NewFarFloor(g *Graph, population []*Entry, draft FarDraft, listed []string) *FarFloor {
	areas := map[string]bool{}
	for _, label := range draft.Topics {
		path, err := ParseTopicPath(label)
		if err == nil {
			areas[caseFoldKey(path.Components[0])] = true
		}
	}
	references := slices.DeleteFunc(slices.Clone(draft.References), func(id string) bool { return id == "" })
	if len(areas) == 0 && len(references) == 0 {
		return nil
	}
	near := withinRefHops(g, references, FarFloorHops)
	skip := map[string]bool{}
	for _, id := range listed {
		skip[id] = true
	}
	var far []*Entry
	rank := map[string]uint64{}
	for _, e := range population {
		if e.Embedded || skip[e.ID] || near[e.ID] || inTopicAreas(g, e, areas) {
			continue
		}
		sum := sha256.Sum256([]byte(draft.Subject + "\nfar\n" + e.ID))
		rank[e.ID] = binary.BigEndian.Uint64(sum[:8])
		far = append(far, e)
	}
	if len(far) < FarFloorSize {
		return nil
	}
	slices.SortFunc(far, func(a, b *Entry) int {
		return cmp.Or(cmp.Compare(rank[a.ID], rank[b.ID]), cmp.Compare(a.ID, b.ID))
	})
	return &FarFloor{Far: slices.Clone(far[:FarFloorSize]), near: near}
}

// Below reports whether a judged candidate lies below the floor: its score
// is at or below line, the line of its own call (FarFloor.Line). A
// candidate within FarFloorHops of an entry the draft references, or found
// by two or more reads, never does.
func (f *FarFloor) Below(candidate GatherCandidate, score, line float64) bool {
	return f != nil && score <= line && !floorSpares(f.near, candidate)
}

// StandingBelowFloor returns the verdicts of held, the candidates a reading
// holds below the floor, that still stand for candidates: one the floor now
// spares (FarFloor.Below), by a reference the draft added or a second read
// that found it, is none of them.
func StandingBelowFloor(g *Graph, references []string, candidates []GatherCandidate, held []string) map[string]bool {
	standing := map[string]bool{}
	if len(held) == 0 {
		return standing
	}
	near := withinRefHops(g, references, FarFloorHops)
	for _, candidate := range candidates {
		if slices.Contains(held, candidate.Entry.ID) && !floorSpares(near, candidate) {
			standing[candidate.Entry.ID] = true
		}
	}
	return standing
}

func floorSpares(near map[string]bool, candidate GatherCandidate) bool {
	return near[candidate.Entry.ID] || len(candidate.FoundBy) >= 2
}

func inTopicAreas(g *Graph, e *Entry, areas map[string]bool) bool {
	for _, topic := range g.EffectiveTopics(e) {
		if areas[caseFoldKey(topic.Components[0])] {
			return true
		}
	}
	return false
}

// withinRefHops returns ids and every entry within hops reference hops of
// one of them: a ref, a close or a supersede, followed in either direction.
func withinRefHops(g *Graph, ids []string, hops int) map[string]bool {
	near := map[string]bool{}
	for _, id := range ids {
		near[id] = true
	}
	frontier := ids
	for range hops {
		var next []string
		for _, id := range frontier {
			neighbours := slices.Concat(g.RefsTo[id], g.ClosedBy[id], g.SupersededBy[id])
			if e := g.ByID[id]; e != nil {
				neighbours = slices.Concat(neighbours, RefIDs(e.Refs), e.Closes, e.Supersedes)
			}
			for _, neighbour := range neighbours {
				if !near[neighbour] {
					near[neighbour] = true
					next = append(next, neighbour)
				}
			}
		}
		frontier = next
	}
	return near
}
