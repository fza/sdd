package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/internal/model"
)

// Repeats applies the no-repeat rule to a serve (20261109-004506-d-cpt-udz,
// R14) under the anchor proof (20261112-075604-d-tac-t4r): an entry the call
// proves it holds prints as its pointer (ID and brief, never cut) instead of
// its line, body or citations, and an entry served in full carries its tag.
// A nil Repeats, or one without Held, serves everything in full, as the CLI
// does.
type Repeats struct {
	// Held reports whether the call holds the entry, with its tag.
	Held func(id string) (tag string, held bool)
	// Anchor returns an entry body served in full with its tag placed.
	Anchor  func(id, body string) string
	pointed bool
}

// pointer returns the pointer an entry prints as when the call holds it. An
// entry without a brief, one of a connected repo's graph, never does.
func (r *Repeats) pointer(id, brief string) (string, bool) {
	if r == nil || r.Held == nil || brief == "" {
		return "", false
	}
	tag, held := r.Held(id)
	if !held {
		return "", false
	}
	r.pointed = true
	return pointerText(id, brief, tag), true
}

// anchor returns body as an entry served in full prints it.
func (r *Repeats) anchor(id, body string) string {
	if r == nil || r.Anchor == nil {
		return body
	}
	return r.Anchor(id, body)
}

// entry writes id's line through write, or its pointer when it is held.
func (r *Repeats) entry(w io.Writer, id, brief string, write func()) {
	if pointer, held := r.pointer(id, brief); held {
		fmt.Fprintf(w, "  %s\n", pointer)
		return
	}
	write()
}

// Pointed reports whether the serve printed a pointer.
func (r *Repeats) Pointed() bool { return r != nil && r.pointed }

// WriteWayBack writes, after a serve that printed a pointer, the call that
// serves a pointed entry in full again (D34).
func (r *Repeats) WriteWayBack(w io.Writer) {
	if r.Pointed() {
		fmt.Fprintf(w, "\nAn entry marked held is not served again while you prove you hold it; to have it in full again, call %s.\n", GatherWayBack)
	}
}

// pointerText is the pointer a held entry prints as: its ID, its brief,
// never cut, and the tag it is held under.
func pointerText(id, brief, tag string) string {
	if tag == "" {
		return fmt.Sprintf("%s — %s (served in full above)", id, brief)
	}
	return fmt.Sprintf("%s — %s (held as %s)", id, brief, model.AnchorMark(tag))
}
