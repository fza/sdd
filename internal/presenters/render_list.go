package presenters

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/application/types"
)

// renderAsList writes one entry-line per entry in the flat list. Reuses
// the canonical entry line shared with `sdd search` so output stays
// consistent across surfaces.
// When the section is ranked (ViewList.Ranked), each line carries a
// `{score: X.XXX}` segment. by(date) sorts without scoring, falling back to
// the plain line — sort without per-entry rendering noise.
//
// When the section used expand(refs) (ViewList.RefsExpanded), each entry's
// resolved outgoing refs render as indented sub-lines beneath it.
func renderAsList(w io.Writer, list types.ViewList, brief bool, r *Repeats) {
	for _, item := range list.Items {
		r.entry(w, item.Entry.ID, item.Entry.Brief, func() {
			switch {
			case brief:
				writeEntryLineBrief(w, item.Entry)
			case list.Ranked:
				writeEntryLine(w, item.Entry, item.Score)
			default:
				writeEntryLine(w, item.Entry, math.NaN())
			}
		})
		if list.RefsExpanded {
			for _, ref := range item.Refs {
				writeRefExpansion(w, ref)
			}
			if item.RefsDropped > 0 {
				fmt.Fprintf(w, "    → +%d more refs\n", item.RefsDropped)
			}
		}
	}
}

// writeSectionCut renders a section's serve-budget cut in the view register:
// the dropped count and the runnable pull for the complete section.
func writeSectionCut(w io.Writer, dropped int, pull string) {
	if dropped <= 0 {
		return
	}
	fmt.Fprintf(w, "(… and %d more — pull: %s)\n", dropped, pull)
}

// writeRefExpansion renders one expand(refs) sub-line beneath its parent
// entry. The per-ref kind becomes the verb (grounded-in, builds-on, addresses,
// …); legacy bare-string refs (kind unknown) render with the generic verb
// `refs`. The referenced entry's derived status follows, then the optional
// per-ref desc as a quoted clause. Done-signal targets carry no status (they
// are terminal facts of execution) — matching how done signals render on
// every other surface, the status segment is simply omitted.
//
//	→ grounds 20260101-100000-d-cpt-aaa {status: active}
//	→ depends-on 20260101-100000-d-tac-bbb {status: closed-by <id>}: "why this ref exists"
func writeRefExpansion(w io.Writer, ref types.ViewRef) {
	var sb strings.Builder
	sb.WriteString("    → ")
	sb.WriteString(refVerb(ref.Kind))
	sb.WriteString(" ")
	sb.WriteString(ref.ID)
	if ref.UnresolvedRepo != "" {
		sb.WriteString(" [unresolved: repo ")
		sb.WriteString(ref.UnresolvedRepo)
		sb.WriteString("]")
	} else if s := formatStatusTrail(ref.DerivedStatus); s != "" {
		sb.WriteString(" ")
		sb.WriteString(s)
	}
	if ref.Desc != "" {
		sb.WriteString(`: "`)
		sb.WriteString(ref.Desc)
		sb.WriteString(`"`)
	}
	sb.WriteString("\n")
	fmt.Fprint(w, sb.String())
}

// refVerb maps a ref kind to its sub-line verb. Unknown (legacy bare-string)
// refs render with the generic `refs` verb so they stay legible without a
// captured kind; every capturable kind renders as its own verb.
func refVerb(kind string) string {
	if kind == string(model.RefKindUnknown) || kind == "" {
		return "refs"
	}
	return kind
}
