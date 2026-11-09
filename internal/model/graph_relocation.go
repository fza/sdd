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

// GraphFolder is what a graph folder holds: whether it exists and how many
// entry files lie in it.
type GraphFolder struct {
	Dir     string
	Exists  bool
	Entries int
}

// GraphRelocation is a change of the graph folder a serving process reads,
// From to To, with the entries From held while it was settled.
type GraphRelocation struct {
	From    string
	To      string
	Entries int
}

// Unsettled returns why the relocation is not settled yet, nil once To holds
// the graph and From holds none of it.
func (r GraphRelocation) Unsettled(to, from GraphFolder) error {
	switch {
	case !to.Exists:
		return fmt.Errorf("graph folder %s does not exist: the config moved the graph there from %s; finish the move or restore the config", r.To, r.From)
	case to.Entries == 0:
		return fmt.Errorf("graph folder %s holds no entries: the config moved the graph there from %s; finish the move or restore the config", r.To, r.From)
	case from.Entries > 0:
		return fmt.Errorf("graph move partial: %d entries are still in %s, the folder served before %s; finish the move", from.Entries, r.From, r.To)
	case to.Entries < r.Entries:
		return fmt.Errorf("graph move partial: %s holds %d entries, %s held %d; finish the move", r.To, to.Entries, r.From, r.Entries)
	}
	return nil
}
