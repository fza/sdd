package presenters

import (
	"fmt"
	"io"
	"strings"

	"github.com/networkteam/sdd/internal/mdcompose"
	"github.com/networkteam/sdd/pkg/application/types"
)

// renderAsBodies writes each entry's full body into the document's heading
// hierarchy at entryLevel — one level below the section header, so a served
// body never outranks the structure serving it (d-cpt-5wv).
//
// Per entry the render emits an identity header and then the body. The summary
// sentence is deliberately absent: it derives from the body, and serving both
// would say the same thing twice. A body that opens with a heading contributes
// that heading as the entry's title, with the identity line as its byline; a
// body opening with prose takes the identity line as its header.
func renderAsBodies(w io.Writer, bodies types.ViewBodies, entryLevel int, r *Repeats) {
	marker := strings.Repeat("#", entryLevel)
	for i, e := range bodies.Entries {
		if i > 0 {
			fmt.Fprintln(w)
		}
		if pointer, held := r.pointer(e.ID, e.Brief); held {
			fmt.Fprintf(w, "%s\n", pointer)
			continue
		}
		identity := bodyIdentity(e)
		title, body := mdcompose.SplitLeadingHeading(strings.TrimSpace(e.Body))
		if title != "" {
			fmt.Fprintf(w, "%s %s\n\n%s\n", marker, title, identity)
		} else {
			fmt.Fprintf(w, "%s %s\n", marker, identity)
		}
		if body = strings.TrimSpace(mdcompose.DemoteTo(body, entryLevel+1)); body != "" {
			body = r.anchor(e.ID, body)
			fmt.Fprintf(w, "\n%s\n", body)
		}
	}
	if len(bodies.Withheld) > 0 {
		if len(bodies.Entries) > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "(withheld for size, %d whole — pull: %s)\n", len(bodies.Withheld), bodies.Pull)
		for _, e := range bodies.Withheld {
			fmt.Fprintf(w, "- %s — %s\n", e.ID, e.Brief)
		}
	}
}

// bodyIdentity renders an entry's identity line: the full ID a reader cites or
// passes to a tool, plus the layer/kind/type words that place it in the graph.
func bodyIdentity(e types.ListedEntry) string {
	words := []string{e.Layer}
	if e.Kind != "" {
		words = append(words, e.Kind)
	}
	words = append(words, e.Type)
	return fmt.Sprintf("`%s` — %s", e.ID, strings.Join(words, " "))
}
