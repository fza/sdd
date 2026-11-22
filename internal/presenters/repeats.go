package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/internal/model"
)

// GatherWayBack is the call a pointer names to have an entry served in full
// again.
const GatherWayBack = "show with its id"

// Repeats applies the no-repeat rule to a serve: an entry the call already
// holds in full prints as its pointer (ID and brief, never cut) instead of
// its line, body or citations. A nil Repeats, or one without Held, serves
// everything in full, as the CLI does.
type Repeats struct {
	// Held reports whether the call holds the entry in full, keyed by its ID
	// and the hash of its stored text (model.EntryHash): served earlier in
	// the same call.
	Held func(id, hash string) bool
	// Served records an entry body served in full.
	Served  func(id, hash, body string)
	pointed bool
}

// pointer returns the pointer an entry prints as when the call holds it. An
// entry without a brief, one of a connected repo's graph, never does.
func (r *Repeats) pointer(e *model.Entry) (string, bool) {
	brief := model.EntryBrief(e)
	if r == nil || r.Held == nil || brief == "" || !r.Held(e.ID, model.EntryHash(e)) {
		return "", false
	}
	r.pointed = true
	return pointerText(e.ID, brief), true
}

// serve returns body as an entry served in full prints it, recording the
// serve.
func (r *Repeats) serve(e *model.Entry, body string) string {
	if r != nil && r.Served != nil {
		r.Served(e.ID, model.EntryHash(e), body)
	}
	return body
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
		fmt.Fprintf(w, "\nAn entry marked served in full is not repeated in this result; to have it again, call %s.\n", GatherWayBack)
	}
}

// pointerText is the pointer a held entry prints as: its ID and its brief,
// never cut.
func pointerText(id, brief string) string {
	return fmt.Sprintf("%s — %s (served in full above)", id, brief)
}
