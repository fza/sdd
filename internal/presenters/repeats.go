package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/internal/model"
)

// Repeats applies the no-repeat rule to a serve (20261109-004506-d-cpt-udz,
// R14): an entry the consumer already holds in full prints as its pointer
// (ID and brief, never cut) instead of its line, body or citations. A nil
// Repeats, or one without Held, serves everything in full, as the CLI does.
type Repeats struct {
	Held    func(id string) bool
	pointed bool
}

func (r *Repeats) holds(id string) bool {
	if r == nil || r.Held == nil || !r.Held(id) {
		return false
	}
	r.pointed = true
	return true
}

// entry writes e's line through write, or its pointer when e is held.
func (r *Repeats) entry(w io.Writer, e *model.Entry, write func()) {
	if r.holds(e.ID) {
		fmt.Fprintf(w, "  %s\n", GatherPointer(e))
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
