package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Recheck items (20261109-004506-d-cpt-udz, D5, D14): strains between
// existing entries and hand edits of committed entries, kept as sdd.recheck
// events in the recheck channel until a later entry heals them.
const (
	RecheckChannel   = "recheck"
	RecheckEventType = ReservedEventNamespace + ".recheck"
	// RecheckBaselineType marks the time the first hand-edit scan ran; edits
	// committed before it predate the recheck list and never become items.
	RecheckBaselineType = ReservedEventNamespace + ".recheck-baseline"
	RecheckStrain       = "strain"
	RecheckHandEdit     = "hand-edit"
)

// Strain relations an agent reports between two existing entries (5.9).
const (
	StrainContradicts = "contradicts"
	StrainDuplicates  = "duplicates"
	StrainOutdates    = "outdates"
)

// StrainRelations lists the strain relations.
func StrainRelations() []string {
	return []string{StrainContradicts, StrainDuplicates, StrainOutdates}
}

// StrainRelationBit is the ledger relation bit of a strain relation.
func StrainRelationBit(relation string) (uint8, error) {
	switch relation {
	case StrainContradicts:
		return RelationContradicts, nil
	case StrainDuplicates:
		return RelationDuplicates, nil
	case StrainOutdates:
		return RelationOutdates, nil
	}
	return 0, fmt.Errorf("unknown strain relation %q (%s)", relation, strings.Join(StrainRelations(), ", "))
}

// Strain is a clash an agent reports between two existing entries.
type Strain struct {
	IDs      [2]string
	Relation string
}

// RecheckItem is one item of the recheck list.
type RecheckItem struct {
	ID   string
	Kind string
	// Entries are the strained pair, sorted, or the edited entry.
	Entries []string
	// Relation names a strain's relation; Commit a hand edit's commit.
	Relation string
	Commit   string
	// ReportedBy is the entry whose capture reported a strain.
	ReportedBy string
	At         time.Time
}

// recheckPayload is the event payload of a recheck item.
type recheckPayload struct {
	Kind       string `json:"kind"`
	Relation   string `json:"relation,omitempty"`
	Commit     string `json:"commit,omitempty"`
	ReportedBy string `json:"reported_by,omitempty"`
}

// StrainItem returns the recheck item of strain, reported by the capture
// of reportedBy at at.
func StrainItem(strain Strain, reportedBy string, at time.Time) RecheckItem {
	entries := []string{strain.IDs[0], strain.IDs[1]}
	slices.Sort(entries)
	return withRecheckID(RecheckItem{Kind: RecheckStrain, Entries: entries, Relation: strain.Relation, ReportedBy: reportedBy, At: at})
}

// HandEditItem returns the recheck item of a commit outside sdd's own that
// changed entry.
func HandEditItem(entry, commit string, at time.Time) RecheckItem {
	return withRecheckID(RecheckItem{Kind: RecheckHandEdit, Entries: []string{entry}, Commit: commit, At: at})
}

// withRecheckID sets the item's ID from what it is about, so a clash or an
// edit found twice is one event and the store keeps one copy.
func withRecheckID(item RecheckItem) RecheckItem {
	sum := sha256.Sum256([]byte(item.Kind + "\x00" + strings.Join(item.Entries, "\x00") + "\x00" + item.Relation + "\x00" + item.Commit))
	item.ID = "recheck-" + hex.EncodeToString(sum[:10])
	return item
}

// Payload is the item's event payload.
func (item RecheckItem) Payload() ([]byte, error) {
	return json.Marshal(recheckPayload{Kind: item.Kind, Relation: item.Relation, Commit: item.Commit, ReportedBy: item.ReportedBy})
}

// RecheckItemFromEvent reads an sdd.recheck event back into its item.
func RecheckItemFromEvent(id string, at time.Time, entries []string, payload []byte) (RecheckItem, error) {
	var decoded recheckPayload
	err := json.Unmarshal(payload, &decoded)
	if err != nil {
		return RecheckItem{}, fmt.Errorf("recheck event %s: %w", id, err)
	}
	item := RecheckItem{ID: id, Kind: decoded.Kind, Entries: slices.Clone(entries), Relation: decoded.Relation,
		Commit: decoded.Commit, ReportedBy: decoded.ReportedBy, At: at}
	switch {
	case item.Kind == RecheckStrain && len(item.Entries) == 2:
	case item.Kind == RecheckHandEdit && len(item.Entries) == 1:
	default:
		return RecheckItem{}, fmt.Errorf("recheck event %s: kind %q with %d entries", id, item.Kind, len(item.Entries))
	}
	return item, nil
}

// healed reports whether a later entry healed the item: one of its entries
// is no longer live, or an entry written after the item, other than the one
// that reported it, cites every one of them. An entry time carries whole
// seconds only, so one written in the second the item was reported counts.
func (item RecheckItem) healed(g *Graph, live map[string]bool) bool {
	for _, id := range item.Entries {
		if !live[id] {
			return true
		}
	}
	for _, e := range g.Entries {
		if e.Time.Before(item.At.Truncate(time.Second)) || e.ID == item.ReportedBy || slices.Contains(item.Entries, e.ID) {
			continue
		}
		if citesAll(e, item.Entries) {
			return true
		}
	}
	return false
}

func citesAll(e *Entry, ids []string) bool {
	for _, id := range ids {
		cited := slices.Contains(e.Supersedes, id) || slices.Contains(e.Closes, id) ||
			slices.ContainsFunc(e.Refs, func(ref Ref) bool { return ref.ID == id })
		if !cited {
			return false
		}
	}
	return true
}

// UnhealedRecheck returns the items no later entry healed, in their order.
func UnhealedRecheck(g *Graph, items []RecheckItem) []RecheckItem {
	live := liveEntries(g)
	var open []RecheckItem
	for _, item := range items {
		if !item.healed(g, live) {
			open = append(open, item)
		}
	}
	return open
}

// HealedRecheck returns the IDs of the items a later entry healed: what
// compaction drops from the recheck channel (D17).
func HealedRecheck(g *Graph, items []RecheckItem) []string {
	live := liveEntries(g)
	var healed []string
	for _, item := range items {
		if item.healed(g, live) {
			healed = append(healed, item.ID)
		}
	}
	return healed
}

func liveEntries(g *Graph) map[string]bool {
	live := map[string]bool{}
	for _, e := range g.Filter(GraphFilter{OpenOnly: true}) {
		live[e.ID] = true
	}
	return live
}

// StrainRefusals returns the unhealed strains a draft citing cited builds
// on (D26): a strain one of whose entries the draft cites, unless the draft
// cites both and so heals it.
func StrainRefusals(open []RecheckItem, cited []string) []RecheckItem {
	var out []RecheckItem
	for _, item := range open {
		if item.Kind != RecheckStrain {
			continue
		}
		hits := 0
		for _, id := range item.Entries {
			if slices.Contains(cited, id) {
				hits++
			}
		}
		if hits > 0 && hits < len(item.Entries) {
			out = append(out, item)
		}
	}
	return out
}

// RecheckConfig is the `recheck` block. Only the committed .sdd/config.yaml
// sets it.
type RecheckConfig struct {
	// Responsible names the participants catch-up shows the recheck list
	// and the healing suggestion to; empty shows them to everyone (D5).
	Responsible []string `yaml:"responsible,omitempty"`
}

func mergeRecheckConfig(base, overlay RecheckConfig) RecheckConfig {
	if len(overlay.Responsible) > 0 {
		return RecheckConfig{Responsible: slices.Clone(overlay.Responsible)}
	}
	return base
}
