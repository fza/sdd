package model

import (
	"fmt"
	"slices"
	"strings"
)

// Verdict codes: why an entry served in full or listed in a capture's reading
// is set aside instead of referenced (20261109-004506-d-cpt-udz, D10, D12).
const (
	VerdictTopicOnly  = "topic-only"
	VerdictOtherScope = "other-scope"
	VerdictTermClash  = "term-clash"
	VerdictCovered    = "covered"
	VerdictMeta       = "meta"
	VerdictOther      = "other"
)

// GroundingVerdictCodes lists the verdict codes in their ledger order.
func GroundingVerdictCodes() []string {
	return []string{VerdictTopicOnly, VerdictOtherScope, VerdictTermClash, VerdictCovered, VerdictMeta, VerdictOther}
}

// ValidGroundingVerdictCode reports whether code is a verdict code.
func ValidGroundingVerdictCode(code string) bool {
	return slices.Contains(GroundingVerdictCodes(), code)
}

// GroundingVerdict sets entries aside with one code.
type GroundingVerdict struct {
	IDs  []string
	Code string
}

// GroundingEvidence is what the capture gate judges: the instance's credited
// reading against the draft and the graph as it stands.
type GroundingEvidence struct {
	Reading *Reading
	Kind    Kind
	// Topics are the draft's topic labels; each owes a listing outside the
	// all-briefs mode.
	Topics []TopicPath
	// Answered are the draft's refs, supersedes and closes.
	Answered []string
	Verdicts []GroundingVerdict
	// Live is the grounding population now (GroundingPopulation).
	Live []string
	// Own are the entries written by the capture's own dialogue; they never
	// count as landed.
	Own []string
}

// Exempt reports whether the draft's kind skips the gate (D38).
func (e GroundingEvidence) Exempt() bool { return GroundingExemptKind(e.Kind) }

// OwedReads names the reads the reading still lacks: none once every live
// brief was served, else the semantic search, a keyword search and the
// listing of each draft topic (D24, D25).
func (e GroundingEvidence) OwedReads() []string {
	if e.Reading.Ran(GroundingBriefs) {
		return nil
	}
	var owed []string
	if !e.Reading.Ran(GroundingSemantic) {
		owed = append(owed, "the semantic search")
	}
	if !e.Reading.Ran(GroundingKeyword) {
		owed = append(owed, "a keyword search")
	}
	for _, topic := range e.Topics {
		if !e.listedTopic(topic.String()) {
			owed = append(owed, fmt.Sprintf("the listing of topic %s", topic))
		}
	}
	return owed
}

func (e GroundingEvidence) listedTopic(label string) bool {
	if e.Reading == nil {
		return false
	}
	for _, read := range e.Reading.Reads {
		if read.Mode == GroundingListing && read.Text == label {
			return true
		}
	}
	return false
}

// Unlisted returns the live entries that landed after the reading began and
// that no credited read listed yet (D13).
func (e GroundingEvidence) Unlisted() []string {
	var out []string
	for _, id := range e.landed() {
		if !e.Reading.IsListed(id) {
			out = append(out, id)
		}
	}
	return out
}

// OpenFullReads returns the entries served in full inside the reading that
// end neither in the draft's refs nor in a verdict (D39).
func (e GroundingEvidence) OpenFullReads() []string {
	answered := e.answered()
	var out []string
	for _, id := range e.Reading.Full() {
		if !answered[id] {
			out = append(out, id)
		}
	}
	return out
}

// UnlistedVerdicts returns the verdict IDs no credited read listed: a verdict
// answers only an entry the reading surfaced.
func (e GroundingEvidence) UnlistedVerdicts() []string {
	var out []string
	for _, verdict := range e.Verdicts {
		for _, id := range verdict.IDs {
			if !e.Reading.IsListed(id) && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Late returns the live entries that landed after the reading began, that no
// read before the write step listed, and that neither the draft nor a
// verdict answers: what the write step still owes (D13, D37).
func (e GroundingEvidence) Late() []string {
	answered := e.answered()
	var out []string
	for _, id := range e.landed() {
		if !answered[id] && !e.Reading.ListedOutside(id, GroundingLate) {
			out = append(out, id)
		}
	}
	return out
}

func (e GroundingEvidence) landed() []string {
	if e.Reading == nil || e.Reading.Corpus == nil {
		return nil
	}
	var out []string
	for _, id := range e.Reading.Corpus.Since(e.Live) {
		if !slices.Contains(e.Own, id) {
			out = append(out, id)
		}
	}
	return out
}

func (e GroundingEvidence) answered() map[string]bool {
	answered := map[string]bool{}
	for _, id := range e.Answered {
		answered[id] = true
	}
	for _, verdict := range e.Verdicts {
		for _, id := range verdict.IDs {
			answered[id] = true
		}
	}
	return answered
}

// Seed block delimiters of the reading a capture's widen report carries.
const (
	readingSeedOpen  = "Reading recorded by sdd gather:"
	readingSeedClose = "End of the recorded reading."
)

// WithReadingSeed returns text with the reading's seed block (D32): every read
// as run with the IDs it surfaced, then the entries served in full. A seed
// already in text is replaced in place; text around it stays as written, so
// inherited text stays above the seed and the agent's prose below it.
func WithReadingSeed(text string, r *Reading) string {
	seed := readingSeed(r)
	lines := strings.Split(text, "\n")
	start, end := -1, -1
	for i, line := range lines {
		if start < 0 && line == readingSeedOpen {
			start = i
		}
		if start >= 0 && line == readingSeedClose {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		base := strings.TrimRight(text, "\n")
		if strings.TrimSpace(base) == "" {
			return seed
		}
		return base + "\n\n" + seed
	}
	lines = slices.Replace(lines, start, end+1, seed)
	return strings.Join(lines, "\n")
}

func readingSeed(r *Reading) string {
	var b strings.Builder
	b.WriteString(readingSeedOpen + "\n")
	if r != nil {
		for _, read := range r.Reads {
			ids := "no entries"
			if len(read.IDs) > 0 {
				ids = strings.Join(read.IDs, ", ")
			}
			fmt.Fprintf(&b, "- %s: %s\n", readTag(read), ids)
		}
		full := r.Full()
		if len(full) > 0 {
			fmt.Fprintf(&b, "- served in full: %s\n", strings.Join(full, ", "))
		}
	}
	b.WriteString(readingSeedClose)
	return b.String()
}
