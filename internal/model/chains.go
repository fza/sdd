package model

import (
	"slices"
	"sort"
)

// CanonicalChain is the supersession chain shared by the canonical-named
// kinds (actor, procedure): its entries, the canonicals they carried, and
// the write-once rule across chains. A canonical may change within a chain
// (e.g. a typo correction supersedes an earlier spelling) but belongs to one
// chain ever; closed chains do not free their canonicals.
type CanonicalChain struct {
	// Entries are all entries in the chain, ordered by time (oldest first).
	Entries []*Entry
	// Heads are the chain's non-superseded entries, oldest first. A linear
	// or merged chain has exactly one; more than one is a fork.
	Heads []*Entry
	// CanonicalHistory is the ordered list of distinct canonicals ever
	// carried by entries in this chain, oldest first.
	CanonicalHistory []string
	// canonicalSet is an internal lookup for membership checks.
	canonicalSet map[string]bool
}

func (g *Graph) newCanonicalChain(group []*Entry) CanonicalChain {
	c := CanonicalChain{Entries: group, canonicalSet: make(map[string]bool)}
	for _, e := range group {
		if e.Canonical != "" && !c.canonicalSet[e.Canonical] {
			c.canonicalSet[e.Canonical] = true
			c.CanonicalHistory = append(c.CanonicalHistory, e.Canonical)
		}
		if len(g.SupersededBy[e.ID]) == 0 {
			c.Heads = append(c.Heads, e)
		}
	}
	return c
}

// HasCanonical reports whether the chain has ever held canonical c.
func (c *CanonicalChain) HasCanonical(name string) bool {
	return c.canonicalSet[name]
}

// ReusedBy reports whether a new entry carrying canonical and superseding the
// given IDs would reuse this chain's canonical from outside it. Superseding
// any member extends the chain, so a merge extends every chain it supersedes.
func (c *CanonicalChain) ReusedBy(canonical string, supersedes []string) bool {
	if !c.HasCanonical(canonical) {
		return false
	}
	return !slices.ContainsFunc(c.Entries, func(e *Entry) bool {
		return slices.Contains(supersedes, e.ID)
	})
}

// chainGroups groups the entries selected by member into supersession
// chains: each group is a connected component of supersedes edges among
// member entries (an edge to or from a non-member never joins two groups),
// so a fork stays one chain and a merge joins the chains it supersedes.
// Each group is ordered oldest first; groups sort by their oldest entry's
// ID. Callers that need a different order (e.g. by head ID) re-sort.
func (g *Graph) chainGroups(member func(*Entry) bool) [][]*Entry {
	seen := make(map[string]bool)
	var groups [][]*Entry
	for _, start := range g.Entries {
		if !member(start) || seen[start.ID] {
			continue
		}
		seen[start.ID] = true
		group := []*Entry{}
		stack := []*Entry{start}
		for len(stack) > 0 {
			e := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			group = append(group, e)
			for _, ids := range [][]string{e.Supersedes, g.SupersededBy[e.ID]} {
				for _, id := range ids {
					n, ok := g.ByID[id]
					if ok && member(n) && !seen[n.ID] {
						seen[n.ID] = true
						stack = append(stack, n)
					}
				}
			}
		}
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].Time.Equal(group[j].Time) {
				return group[i].ID < group[j].ID
			}
			return group[i].Time.Before(group[j].Time)
		})
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i][0].ID < groups[j][0].ID
	})
	return groups
}
