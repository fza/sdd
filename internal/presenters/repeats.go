package presenters

import (
	"fmt"
	"io"
)

// Repeats applies the no-repeat rule to a serve (20261109-004506-d-cpt-udz,
// R14): an entry the consumer already holds in full prints as its pointer
// (ID and brief, never cut) instead of its line, body or citations. A nil
// Repeats, or one without Held, serves everything in full, as the CLI does.
type Repeats struct {
	Held    func(id string) bool
	pointed bool
}

// holds reports whether the entry prints as its pointer. An entry without a
// brief, one of a connected repo's graph, never does.
func (r *Repeats) holds(id, brief string) bool {
	if r == nil || r.Held == nil || brief == "" || !r.Held(id) {
		return false
	}
	r.pointed = true
	return true
}

// entry writes id's line through write, or its pointer when it is held.
func (r *Repeats) entry(w io.Writer, id, brief string, write func()) {
	if r.holds(id, brief) {
		fmt.Fprintf(w, "  %s\n", pointerText(id, brief))
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
		fmt.Fprintf(w, "\nAn entry marked served in full is not served again; to have it served in full, call %s.\n", GatherWayBack)
	}
}

// pointerText is the pointer an already-served entry prints as: its ID, its
// brief, never cut, and a marker naming the way back.
func pointerText(id, brief string) string {
	return fmt.Sprintf("%s — %s (served in full above)", id, brief)
}
