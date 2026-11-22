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
	// the same call, or sent by the result before a deferral follows.
	Held func(id, hash string) bool
	// Served records an entry body served in full.
	Served func(id, hash, body string)
	// Graph holds the entries the serve renders, whose stored text keys
	// them; an entry it does not hold keys on its ID alone.
	Graph   *model.Graph
	pointed bool
}

// pointer returns the pointer an entry prints as when the call holds it. An
// entry without a brief, one of a connected repo's graph, never does.
func (r *Repeats) pointer(id, brief string) (string, bool) {
	if r == nil || r.Held == nil || brief == "" || !r.Held(id, r.hash(id)) {
		return "", false
	}
	r.pointed = true
	return pointerText(id, brief), true
}

// serve returns body as an entry served in full prints it, recording the
// serve.
func (r *Repeats) serve(id, body string) string {
	if r != nil && r.Served != nil {
		r.Served(id, r.hash(id), body)
	}
	return body
}

func (r *Repeats) hash(id string) string {
	if r.Graph == nil {
		return ""
	}
	e, ok := r.Graph.ByID[id]
	if !ok {
		return ""
	}
	return model.EntryHash(e)
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
// serves a pointed entry in full again.
func (r *Repeats) WriteWayBack(w io.Writer) {
	if r.Pointed() {
		fmt.Fprintf(w, "\nAn entry marked served in full is not repeated in this result; to have it again, call %s.\n", GatherWayBack)
	}
}

// pointerText is the pointer a held entry prints as: its ID and its brief,
// never cut.
func pointerText(id, brief string) string {
	return fmt.Sprintf("%s — %s (served in full above or in the result before)", id, brief)
}
