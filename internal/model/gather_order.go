package model

import (
	"cmp"
	"slices"
)

// gatherFusionK is the constant of the reciprocal rank fusion over a
// check's searches, the one `sdd search` fuses its hybrid mode with.
const gatherFusionK = 60

// OrderGatherCandidates puts a check's candidates in the merged order the
// ranking judge reads them in. An entry a search found ranks by reciprocal
// rank fusion over the ranks the searches gave it; the entries no search
// found, a topic listing's alone, follow by falling similarity to the draft,
// those without one last. Within equal places an entry one reference hop
// from an entry the draft references comes first and an entry of kind done
// last, the rest in the order given. Links and entry dates change no place
// beyond that.
func OrderGatherCandidates(g *Graph, candidates []GatherCandidate, reads []GroundingRead, similarity map[string]float64, references []string) []GatherCandidate {
	fused := map[string]float64{}
	for _, read := range reads {
		if read.Mode != GroundingSemantic && read.Mode != GroundingKeyword {
			continue
		}
		for rank, id := range read.IDs {
			fused[id] += 1 / float64(gatherFusionK+rank+1)
		}
	}
	near := withinRefHops(g, references, 1)
	ordered := slices.Clone(candidates)
	slices.SortStableFunc(ordered, func(a, b GatherCandidate) int {
		x, y := a.Entry.ID, b.Entry.ID
		fusedX, searchedX := fused[x]
		fusedY, searchedY := fused[y]
		if c := cmp.Compare(rankLast(!searchedX), rankLast(!searchedY)); c != 0 {
			return c
		}
		if searchedX && fusedX != fusedY {
			return cmp.Compare(fusedY, fusedX)
		}
		if !searchedX {
			similarX, knownX := similarity[x]
			similarY, knownY := similarity[y]
			if c := cmp.Compare(rankLast(!knownX), rankLast(!knownY)); c != 0 {
				return c
			}
			if similarX != similarY {
				return cmp.Compare(similarY, similarX)
			}
		}
		if c := cmp.Compare(rankLast(!near[x]), rankLast(!near[y])); c != 0 {
			return c
		}
		if c := cmp.Compare(rankLast(a.Entry.Kind == KindDone), rankLast(b.Entry.Kind == KindDone)); c != 0 {
			return c
		}
		return 0
	})
	return ordered
}

// Unsearched returns the IDs of the candidates no search of reads found:
// those whose place in the merged order their similarity to the draft sets.
func Unsearched(candidates []GatherCandidate, reads []GroundingRead) []string {
	searched := map[string]bool{}
	for _, read := range reads {
		if read.Mode == GroundingSemantic || read.Mode == GroundingKeyword {
			for _, id := range read.IDs {
				searched[id] = true
			}
		}
	}
	var out []string
	for _, candidate := range candidates {
		if !searched[candidate.Entry.ID] {
			out = append(out, candidate.Entry.ID)
		}
	}
	return out
}

// RefinedBy returns the IDs of the entries that refine id, in graph order:
// the newer refinements a check's line names.
func RefinedBy(g *Graph, id string) []string {
	var out []string
	for _, other := range g.RefsTo[id] {
		e := g.ByID[other]
		if e == nil || slices.Contains(out, other) {
			continue
		}
		if slices.ContainsFunc(e.Refs, func(ref Ref) bool { return ref.ID == id && ref.Kind == RefKindRefines }) {
			out = append(out, other)
		}
	}
	return out
}
