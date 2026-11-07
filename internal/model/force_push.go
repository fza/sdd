package model

import (
	"fmt"
	"slices"
	"strings"
)

// SyncBlockedSetAside holds entries an upstream force push set aside in the
// sdd-owned graph clone until an agent re-captures or discards each
// (20261106-202055-d-tac-eyl).
const SyncBlockedSetAside SyncBlockedKind = "set-aside"

// SetAside is what upstream force pushes set aside in the sdd-owned graph
// clone: the tips of the latest rewrite, the directory holding the files, and
// the set-aside entries as repository paths.
type SetAside struct {
	Old     string
	New     string
	Dir     string
	Entries []string
}

// Block is the blocked-sync record that carries the set-aside entries to an
// agent.
func (s SetAside) Block() SyncBlocked {
	return SyncBlocked{Kind: SyncBlockedSetAside, Source: s.Old, Destination: s.New, Detail: s.Dir, Paths: s.Entries}
}

// ForcePushNotice names a rewrite of the clone's upstream.
func ForcePushNotice(old, upstream string) string {
	return fmt.Sprintf("the graph repository's upstream was force-pushed from %s to %s; sdd moved its clone onto %s and set aside the entries it had not pushed",
		shortCommit(old), shortCommit(upstream), shortCommit(upstream))
}

// ReaddsAfterForcePush reports whether a set-aside entry returns on its own:
// it supersedes an entry or refines one, and every entry it names is in g.
// Any other entry needs an agent to judge whether it still holds.
func ReaddsAfterForcePush(e *Entry, g *Graph) bool {
	named := slices.Concat(e.Supersedes, e.Closes)
	refines := len(e.Supersedes) > 0
	for _, ref := range e.Refs {
		named = append(named, ref.ID)
		refines = refines || ref.Kind == RefKindRefines
	}
	if !refines {
		return false
	}
	for _, id := range named {
		if g.ByID[id] == nil {
			return false
		}
	}
	return true
}

// RederiveFields carries sdd's change of an entry's derived fields, from
// before to after, onto current, the entry as the rewritten upstream holds
// it. A field sdd left alone keeps current's value.
func RederiveFields(before, after, current string) string {
	beforeFields, ok := FrontmatterFields(before, MergeResolvedKeys)
	if !ok {
		return current
	}
	afterFields, ok := FrontmatterFields(after, MergeResolvedKeys)
	if !ok {
		return current
	}
	for _, key := range MergeResolvedKeys {
		if afterFields[key].sameValue(beforeFields[key]) {
			continue
		}
		current = setFrontmatterField(current, key, afterFields[key].Lines)
	}
	return current
}

// setFrontmatterField states key by lines in content's frontmatter, replacing
// the lines stating it or, when none do, adding lines last; empty lines
// remove the key.
func setFrontmatterField(content, key, lines string) string {
	all, regions, ok := frontmatterRegions(content)
	if !ok {
		return content
	}
	_, found := regions[key]
	if found || lines == "" {
		return ReplaceFrontmatterField(content, key, lines)
	}
	for i := 1; i < len(all); i++ {
		if isDelimiter(all[i]) {
			return strings.Join(all[:i], "") + lines + strings.Join(all[i:], "")
		}
	}
	return content
}

// EntryPathID is the ID of the entry at a repository path ending in
// YYYY/MM/DD-HHmmss-type-layer-suffix.md.
func EntryPathID(repoPath string) (string, error) {
	parts := strings.Split(repoPath, "/")
	if len(parts) < 3 {
		return "", fmt.Errorf("expected a path ending in YYYY/MM/filename.md, got %q", repoPath)
	}
	return RelPathToID(strings.Join(parts[len(parts)-3:], "/"))
}
