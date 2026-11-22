package model

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strconv"
)

// GroundingReadMode names how one grounding read selected the entries it
// surfaced.
type GroundingReadMode string

const (
	// GroundingSemantic is a vector search; Text holds the query, empty for
	// the search with the draft's own text.
	GroundingSemantic GroundingReadMode = "semantic"
	// GroundingKeyword is a text search; Text holds the term.
	GroundingKeyword GroundingReadMode = "keyword"
	// GroundingListing is the live listing of one topic; Text holds the label.
	GroundingListing GroundingReadMode = "listing"
	// GroundingBriefs is every live brief of a small graph, which an older sdd
	// served in place of searching; read only from its session logs.
	GroundingBriefs GroundingReadMode = "briefs"
	// GroundingOpened is entries opened in full by ID.
	GroundingOpened GroundingReadMode = "opened"
	// GroundingNew is the live entries that landed after the reading began,
	// listed by a later gather.
	GroundingNew GroundingReadMode = "new"
	// GroundingLate is the entries that landed after the assemble check,
	// listed at the write step.
	GroundingLate GroundingReadMode = "late"
)

// GroundingRead is one read as it ran: its mode, its query text, term or
// label verbatim, and the entry IDs it surfaced.
type GroundingRead struct {
	Mode GroundingReadMode `json:"mode"`
	Text string            `json:"text,omitempty"`
	IDs  []string          `json:"ids,omitempty"`
}

// CorpusMark is the live population a reading began against: the sorted IDs,
// with their count and set hash for the fast comparison.
type CorpusMark struct {
	IDs   []string `json:"ids"`
	Count int      `json:"count"`
	Hash  string   `json:"hash"`
}

// NewCorpusMark marks the live population ids.
func NewCorpusMark(ids []string) *CorpusMark {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	return &CorpusMark{IDs: sorted, Count: len(sorted), Hash: idSetHash(sorted)}
}

// Since returns the IDs of live that the mark does not hold, sorted: entries
// that landed after the mark was taken, an older ID arriving by merge
// included.
func (m *CorpusMark) Since(live []string) []string {
	if m == nil {
		return nil
	}
	sorted := slices.Clone(live)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	if len(sorted) == m.Count && idSetHash(sorted) == m.Hash {
		return nil
	}
	var fresh []string
	for _, id := range sorted {
		if _, held := slices.BinarySearch(m.IDs, id); !held {
			fresh = append(fresh, id)
		}
	}
	return fresh
}

func idSetHash(sorted []string) string {
	h := sha256.New()
	for _, id := range sorted {
		h.Write([]byte(id))
		h.Write([]byte{'\n'})
	}
	h.Write([]byte(strconv.Itoa(len(sorted))))
	return hex.EncodeToString(h.Sum(nil))
}

// ReadPayload is what one credited read adds to a capture's reading: the
// reads as run, the entries served in full, the entries printed as briefs,
// and, on the first credited read, the corpus mark.
type ReadPayload struct {
	Reads []GroundingRead `json:"reads,omitempty"`
	Full  []string        `json:"full,omitempty"`
	// Shown names the entries printed as a brief or a pointer line, never
	// those printed only as a set-aside ID or count.
	Shown []string `json:"shown,omitempty"`
	// Marked names the lines marked as likely bearing, which owe an answer.
	Marked []string    `json:"marked,omitempty"`
	Corpus *CorpusMark `json:"corpus,omitempty"`
	// Prefilter carries the scores the prefilter gave in this read.
	Prefilter *PrefilterPayload `json:"prefilter,omitempty"`
	// Shortfall records a judge that scored fewer candidates than the
	// check's minimum.
	Shortfall *JudgeShortfall `json:"shortfall,omitempty"`
	// Draft is the draft a check read against.
	Draft *DraftMark `json:"draft,omitempty"`
}

// Empty reports whether the payload carries nothing to record.
func (p ReadPayload) Empty() bool {
	return len(p.Reads) == 0 && len(p.Full) == 0 && len(p.Shown) == 0 && len(p.Marked) == 0 && p.Corpus == nil && p.Prefilter == nil && p.Shortfall == nil && p.Draft == nil
}

// Reading is the record of the grounding reads that credit one capture
// instance: only reads made inside the instance fold in.
type Reading struct {
	Reads  []GroundingRead
	Corpus *CorpusMark
	listed map[string]bool
	full   map[string]bool
	shown  map[string]bool
	marked map[string]bool
	scores map[string]*readingScores
	audit  map[string]bool
	// lastDraft is the draft the latest check read against; judgedDraft the
	// draft as judged (DraftMark).
	lastDraft, judgedDraft *DraftMark
}

// Fold adds one credited read. The first corpus mark stands; later marks are
// ignored, so the reading's start never moves.
func (r *Reading) Fold(p ReadPayload) {
	if r.listed == nil {
		r.listed = map[string]bool{}
		r.full = map[string]bool{}
		r.shown = map[string]bool{}
	}
	for _, read := range p.Reads {
		r.Reads = append(r.Reads, GroundingRead{Mode: read.Mode, Text: read.Text, IDs: slices.Clone(read.IDs)})
		for _, id := range read.IDs {
			if id != "" {
				r.listed[id] = true
			}
		}
	}
	for _, id := range p.Full {
		if id != "" {
			r.full[id] = true
			r.listed[id] = true
		}
	}
	for _, id := range p.Shown {
		if id != "" {
			r.shown[id] = true
		}
	}
	if r.marked == nil {
		r.marked = map[string]bool{}
	}
	for _, id := range p.Marked {
		if id != "" {
			r.marked[id] = true
		}
	}
	r.foldPrefilter(p.Prefilter)
	r.foldDraft(p.Draft)
	if r.Corpus == nil && p.Corpus != nil {
		r.Corpus = p.Corpus
	}
}

// Listed returns every entry a credited read surfaced, sorted.
func (r *Reading) Listed() []string {
	if r == nil {
		return nil
	}
	return sortedKeys(r.listed)
}

// Marked returns every line the reading marked as likely bearing, sorted.
func (r *Reading) Marked() []string {
	if r == nil {
		return nil
	}
	return sortedKeys(r.marked)
}

// Full returns every entry served in full inside the reading, sorted.
func (r *Reading) Full() []string {
	if r == nil {
		return nil
	}
	return sortedKeys(r.full)
}

// Shown returns every entry the reading showed the agent, sorted: served in
// full, or printed as a brief or pointer line.
func (r *Reading) Shown() []string {
	if r == nil {
		return nil
	}
	shown := maps.Clone(r.full)
	if shown == nil {
		shown = map[string]bool{}
	}
	maps.Copy(shown, r.shown)
	return sortedKeys(shown)
}

// IsListed reports whether a credited read surfaced id.
func (r *Reading) IsListed(id string) bool {
	return r != nil && r.listed[id]
}

// ListedOutside reports whether a credited read of another mode than mode
// surfaced id.
func (r *Reading) ListedOutside(id string, mode GroundingReadMode) bool {
	if r == nil {
		return false
	}
	if r.full[id] {
		return true
	}
	for _, read := range r.Reads {
		if read.Mode != mode && slices.Contains(read.IDs, id) {
			return true
		}
	}
	return false
}

// Ran reports whether the reading holds a read of mode.
func (r *Reading) Ran(mode GroundingReadMode) bool {
	if r == nil {
		return false
	}
	for _, read := range r.Reads {
		if read.Mode == mode {
			return true
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
