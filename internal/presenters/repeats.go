package presenters

import (
	"fmt"
	"io"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// Repeats applies the no-repeat rule to a serve under the anchor proof: an
// entry the call proves it holds prints as its pointer (ID and brief, never
// cut) instead of its line, body or citations, and an entry served in full
// carries its tag. A nil Repeats, or one without Held, serves everything in
// full, as the CLI does.
type Repeats struct {
	// Held reports whether the call holds the entry, with its tag and the
	// word whose follower its next proof names (model.Challenge).
	Held func(id string) (tag, next string, held bool)
	// Anchor returns an entry body served in full with its tag placed, its
	// proofs asking for words once in the result, whose letter runs field
	// counts (model.PlaceAnchorIn).
	Anchor  func(id, body string, field map[string]int) string
	pointed bool
	field   model.WordCounts
	// briefs, on RenderWithin's first pass, collects the brief of every entry
	// that could print as its pointer.
	briefs *strings.Builder
}

// pointer returns the pointer an entry prints as when the call holds it. An
// entry without a brief, one of a connected repo's graph, never does.
func (r *Repeats) pointer(id, brief string) (string, bool) {
	if r == nil || brief == "" {
		return "", false
	}
	if r.briefs != nil {
		r.briefs.WriteString(brief + "\n")
		return "", false
	}
	if r.Held == nil {
		return "", false
	}
	tag, next, held := r.Held(id)
	if !held {
		return "", false
	}
	r.pointed = true
	return pointerText(id, brief, tag, next), true
}

// anchor returns body as an entry served in full prints it.
func (r *Repeats) anchor(id, body string) string {
	if r == nil || r.Anchor == nil {
		return body
	}
	return r.Anchor(id, body, r.field)
}

// RenderWithin renders a result through render under r, the entries it
// serves in full anchored over the whole result text. A first pass, which
// asks Held nothing and places no tag, prints every entry in full and every
// brief a pointer could carry, so a proof's prompt occurs once in what the
// agent reads whichever entries the call holds.
func RenderWithin(w io.Writer, r *Repeats, render func(io.Writer, *Repeats) error) error {
	if r != nil && r.Anchor != nil {
		var whole, briefs strings.Builder
		if err := render(&whole, &Repeats{briefs: &briefs}); err != nil {
			return err
		}
		whole.WriteString("\n" + briefs.String())
		whole.WriteString(pointerText("", "", "", "") + "\n")
		whole.WriteString(pointerText("", "", "abcd", "x") + "\n")
		(&Repeats{pointed: true}).WriteWayBack(&whole)
		r.field = model.CountWords(whole.String())
	}
	return render(w, r)
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
		fmt.Fprintf(w, "\nAn entry marked held is not served again while you prove you hold it; to have it in full again, call %s.\n", GatherWayBack)
	}
}

// pointerText is the pointer a held entry prints as: its ID, its brief,
// never cut, the tag it is held under and how the next call proves it.
func pointerText(id, brief, tag, next string) string {
	if tag == "" {
		return fmt.Sprintf("%s — %s (served in full above)", id, brief)
	}
	if proof := model.NextProof(tag, next); proof != "" {
		return fmt.Sprintf("%s — %s (held as %s; %s)", id, brief, model.TagName(tag), proof)
	}
	return fmt.Sprintf("%s — %s (held as %s)", id, brief, model.TagName(tag))
}
