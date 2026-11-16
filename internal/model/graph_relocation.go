package model

import (
	"fmt"
	"path/filepath"
	"regexp"
)

var entryPathRE = regexp.MustCompile(`^\d{4}/\d{2}/\d{2}-\d{6}-[a-z]+-[a-z]+-[0-9a-z]+\.md$`)

// EntryShaped reports whether rel, relative to a graph folder, is shaped like
// an entry file: YYYY/MM/DD-HHmmss-<type>-<layer>-<suffix>.md.
func EntryShaped(rel string) bool {
	return entryPathRE.MatchString(filepath.ToSlash(rel))
}

// GraphFolder is what a graph folder holds: whether it exists and the entry
// files in it, as paths relative to it.
type GraphFolder struct {
	Dir     string
	Exists  bool
	Entries []string
}

// GraphRelocation is a change of the graph folder a serving process reads,
// From to To, with the entries From held while it was settled.
type GraphRelocation struct {
	From    string
	To      string
	Entries int
}

// Unsettled returns why the relocation is not settled yet, nil once To holds
// the graph and every entry From holds. From may keep its entries: a checkout
// of the graph repository stays beside the sdd-owned clone it moved to.
func (r GraphRelocation) Unsettled(to, from GraphFolder) error {
	held := make(map[string]bool, len(to.Entries))
	for _, entry := range to.Entries {
		held[entry] = true
	}
	missing := 0
	for _, entry := range from.Entries {
		if !held[entry] {
			missing++
		}
	}
	switch {
	case !to.Exists:
		return fmt.Errorf("graph folder %s does not exist: the config moved the graph there from %s; finish the move or restore the config", r.To, r.From)
	case len(to.Entries) == 0:
		return fmt.Errorf("graph folder %s holds no entries: the config moved the graph there from %s; finish the move or restore the config", r.To, r.From)
	case missing > 0:
		return fmt.Errorf("graph move partial: %d entries in %s, the folder served before, are missing from %s; finish the move by running `sdd move` again with the same target", missing, r.From, r.To)
	case len(to.Entries) < r.Entries:
		return fmt.Errorf("graph move partial: %s holds %d entries, %s held %d; finish the move by running `sdd move` again with the same target", r.To, len(to.Entries), r.From, r.Entries)
	}
	return nil
}
