package model

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"strings"
	"time"
)

// Replay constants: the most candidates one replayed check holds, the large
// size of the measurement behind the far floor (20261121-205340-d-tac-gyk),
// and the weight of an unreferenced candidate's label, which no reader gave.
const (
	ReplayLines          = 764
	ReplayNegativeWeight = WeightImplicit
)

// ReplayCheck is a past entry replayed as a whole check in the background:
// the entry played the draft, the entries with earlier IDs were its
// candidates, and Judge, at prompt Revision, scored them. GraphSize is the
// live population when the replay ran, which the proof's expiry reads.
// Planted names the needed entry hidden on purpose to test the scan after
// writing, Caught whether the scan noticed it.
type ReplayCheck struct {
	Subject    string            `json:"subject"`
	Judge      string            `json:"judge"`
	Revision   string            `json:"revision,omitempty"`
	At         time.Time         `json:"at"`
	GraphSize  int               `json:"graph_size"`
	Candidates []ReplayCandidate `json:"candidates"`
	Planted    string            `json:"planted,omitempty"`
	Caught     bool              `json:"caught,omitempty"`
}

// ReplayCandidate is one candidate of a replayed check: its score, the line
// its call's far entries set in both forms (Far false when the check had no
// far floor), whether the entry played the draft referenced it (Needed),
// and whether the far floor spares it, found by two or more reads.
type ReplayCandidate struct {
	ID     string `json:"id"`
	Score  uint8  `json:"score"`
	Far    bool   `json:"far,omitempty"`
	Lowest uint8  `json:"lowest,omitempty"`
	Median uint8  `json:"median,omitempty"`
	Needed bool   `json:"needed,omitempty"`
	Spared bool   `json:"spared,omitempty"`
}

// ReplayNeeded returns the entries e refs, closes or supersedes that a
// replay of e can hold as candidates: those the graph holds, with earlier
// IDs, neither exempt nor shipped in the binary.
func ReplayNeeded(g *Graph, e *Entry) []string {
	var needed []string
	for _, id := range slices.Concat(RefIDs(e.Refs), e.Closes, e.Supersedes) {
		other := g.ByID[id]
		if other == nil || id >= e.ID || other.Embedded || GroundingExemptKind(other.Kind) || slices.Contains(needed, id) {
			continue
		}
		needed = append(needed, id)
	}
	slices.Sort(needed)
	return needed
}

// ReplayPopulation returns the entries a replay of e reads: the graph as of
// its writing, the entries with earlier IDs, neither exempt nor shipped.
func ReplayPopulation(g *Graph, e *Entry) []*Entry {
	var earlier []*Entry
	for _, other := range g.Entries {
		if other.ID < e.ID && !other.Embedded && !GroundingExemptKind(other.Kind) {
			earlier = append(earlier, other)
		}
	}
	return earlier
}

// ReplayText is the text a replay of e reads as its draft: its relevance
// text without every sentence that names an entry it references, by full or
// short ID, so the text does not point at its own answers.
func ReplayText(g *Graph, e *Entry) string {
	var names []string
	for _, id := range slices.Concat(RefIDs(e.Refs), e.Closes, e.Supersedes) {
		names = append(names, id, ShortID(id))
	}
	var kept []string
	for _, line := range strings.Split(e.Content, "\n") {
		var sentences []string
		for _, sentence := range splitSentences(line) {
			if !slices.ContainsFunc(names, func(name string) bool { return name != "" && strings.Contains(sentence, name) }) {
				sentences = append(sentences, sentence)
			}
		}
		kept = append(kept, strings.Join(sentences, " "))
	}
	var topics []string
	for _, topic := range g.EffectiveTopics(e) {
		topics = append(topics, topic.String())
	}
	return RelevanceText(string(e.Kind), topics, e.Title, strings.Join(kept, "\n"))
}

// PickReplays returns up to n entries of population to replay, past done:
// entries with at least one needed entry (ReplayNeeded), spread over topic
// areas and months by taking one from each in turn, in an order drawn from
// the entry ID, never by reference count.
func PickReplays(g *Graph, population []*Entry, done map[string]bool, n int) []*Entry {
	buckets := map[string][]*Entry{}
	for _, e := range population {
		if done[e.ID] || e.Embedded || GroundingExemptKind(e.Kind) || len(ReplayNeeded(g, e)) == 0 {
			continue
		}
		area := ""
		if topics := g.EffectiveTopics(e); len(topics) > 0 {
			area = caseFoldKey(topics[0].Components[0])
		}
		key := area + "\n" + e.ID[:min(6, len(e.ID))]
		buckets[key] = append(buckets[key], e)
	}
	keys := slices.Sorted(func(yield func(string) bool) {
		for key := range buckets {
			if !yield(key) {
				return
			}
		}
	})
	for _, key := range keys {
		slices.SortFunc(buckets[key], func(a, b *Entry) int {
			return cmp.Or(cmp.Compare(replayRank(a.ID), replayRank(b.ID)), cmp.Compare(a.ID, b.ID))
		})
	}
	var picked []*Entry
	for round := 0; len(picked) < n; round++ {
		took := false
		for _, key := range keys {
			if round < len(buckets[key]) && len(picked) < n {
				picked = append(picked, buckets[key][round])
				took = true
			}
		}
		if !took {
			break
		}
	}
	return picked
}

func replayRank(id string) uint64 {
	sum := sha256.Sum256([]byte("replay\n" + id))
	return binary.BigEndian.Uint64(sum[:8])
}

// ReplayPlant picks the needed entry a replay hides on purpose, drawn from
// the subject; empty without one.
func ReplayPlant(subject string, needed []string) string {
	if len(needed) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(subject + "\nplant"))
	return needed[binary.BigEndian.Uint64(sum[:8])%uint64(len(needed))]
}

// ReplayLabels are judge's labels from its replayed checks at revision: a
// needed candidate is positive at full weight, every other candidate
// negative at ReplayNegativeWeight, both of SourceCheck.
func ReplayLabels(checks []ReplayCheck, judge, revision string) []RelevanceLabel {
	var labels []RelevanceLabel
	for _, check := range checks {
		if check.Judge != judge || check.Revision != revision {
			continue
		}
		for _, candidate := range check.Candidates {
			label := RelevanceLabel{Pair: NewRelevancePair(check.Subject, candidate.ID), Score: candidate.Score, Kind: LabelNegative, Source: SourceCheck, Weight: ReplayNegativeWeight}
			if candidate.Needed {
				label.Kind, label.Weight = LabelPositive, WeightReferenced
			}
			labels = append(labels, label)
		}
	}
	return labels
}

// CheckedLabels are the labels a judge's thresholds rest on: the ledger's
// live labels and the labels of the judge's replayed checks at revision.
// The labels seeded from the graph's own references drop out: a seeding
// call holds no line that does not bear, unlike a live check.
func CheckedLabels(ledger []RelevanceLabel, replays []ReplayCheck, judge, revision string) []RelevanceLabel {
	return append(LiveLabels(ledger), ReplayLabels(replays, judge, revision)...)
}
