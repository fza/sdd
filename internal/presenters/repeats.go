package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/internal/model"
)

// GatherWayBack is the call a pointer names to have an entry served in full
// again.
const GatherWayBack = "show or gather with its id and its tag left out of held"

// Repeats applies the no-repeat rule to a serve under the anchor proof: an
// entry the call proves it holds prints as its pointer (ID and brief, never
// cut) instead of its line, body or citations, and an entry served in full
// carries its tag. A nil Repeats, or one without Held, serves everything in
// full, as the CLI does.
type Repeats struct {
	// Held reports whether the call holds the entry, keyed by its ID and the
	// hash of its stored text (model.EntryHash), with its tag and the word
	// whose follower its next proof names (model.Challenge).
	Held func(id, hash string) (tag, next string, held bool)
	// Anchor returns an entry body served in full with its tag placed.
	Anchor  func(id, hash, body string) string
	pointed bool
}

// pointer returns the pointer an entry prints as when the call holds it. An
// entry without a brief, one of a connected repo's graph, never does.
func (r *Repeats) pointer(e *model.Entry) (string, bool) {
	brief := model.EntryBrief(e)
	if r == nil || r.Held == nil || brief == "" {
		return "", false
	}
	tag, next, held := r.Held(e.ID, model.EntryHash(e))
	if !held {
		return "", false
	}
	r.pointed = true
	return pointerText(e.ID, brief, tag, next), true
}

// anchor returns body as an entry served in full prints it.
func (r *Repeats) anchor(e *model.Entry, body string) string {
	if r == nil || r.Anchor == nil {
		return body
	}
	return r.Anchor(e.ID, model.EntryHash(e), body)
}

// entry writes e's line through write, or its pointer when it is held.
func (r *Repeats) entry(w io.Writer, e *model.Entry, write func()) {
	if pointer, held := r.pointer(e); held {
		fmt.Fprintf(w, "  %s\n", pointer)
		return
	}
	write()
}

// Pointed reports whether the serve printed a pointer.
func (r *Repeats) Pointed() bool { return r != nil && r.pointed }

// WriteWayBack writes, after a serve that printed a pointer, the call that
// serves a pointed entry in full again.
func (r *Repeats) WriteWayBack(w io.Writer) {
	if r.Pointed() {
		fmt.Fprintf(w, "\nAn entry marked held is not served again while you prove you hold it; to have it in full again, call %s.\n", GatherWayBack)
	}
}

// GatherPointer is the pointer an entry held under tag prints as.
func GatherPointer(e *model.Entry, tag string) string {
	return pointerText(e.ID, model.EntryBrief(e), tag, "")
}

// pointerText is the pointer a held entry prints as: its ID, its brief,
// never cut, the tag it is held under and the word whose follower proves
// it next.
func pointerText(id, brief, tag, next string) string {
	if tag == "" {
		return fmt.Sprintf("%s — %s (served in full above)", id, brief)
	}
	if proof := model.NextProof(tag, next); proof != "" {
		return fmt.Sprintf("%s — %s (held as %s; %s)", id, brief, model.AnchorMark(tag), proof)
	}
	return fmt.Sprintf("%s — %s (held as %s)", id, brief, model.AnchorMark(tag))
}
