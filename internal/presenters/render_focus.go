package presenters

import (
	"fmt"
	"io"
	"strings"

	"github.com/networkteam/sdd/pkg/application/types"
)

// renderAsFocusBlock writes one `### <focus entry-line>` per active
// focus, with each involvement target listed underneath as an indented
// `{state: ...}`-prefixed entry line. Per-focus headers carry the
// focus's own entry line so readers see the focus's title, layer, and
// status without a separate lookup. The state segment surfaces the
// pull-available / stalled / driving classification derived per
// d-tac-uww §6.
//
// Slice 7 keeps the format minimal: focus header, optional `when:` and
// `actors:` lines, then per-target entry lines with state and score.
// The `name(...)` modifier (handled by RenderView) supplies an outer
// `## <title>` header when present.
func renderAsFocusBlock(w io.Writer, block types.ViewFocusBlock, brief bool) {
	for i, focus := range block.Focuses {
		if i > 0 {
			fmt.Fprintln(w)
		}
		// Focus header: `###` followed by the focus's own entry line so
		// the reader gets identity + status + summary without ceremony.
		fmt.Fprint(w, "### ")
		fmt.Fprint(w, focusHeaderLine(focus.Focus, brief))
		writeFocusDefaults(w, focus)
		for _, target := range focus.Targets {
			writeFocusTarget(w, target, brief)
		}
	}
	if block.Dropped > 0 {
		fmt.Fprintln(w)
		writeSectionCut(w, block.Dropped, block.Pull)
	}
}

// focusHeaderLine renders the focus entry as a single line — same shape
// as the entry line but without the leading two-space indent so it can sit
// after the `### ` marker.
func focusHeaderLine(focus *types.ListedEntry, brief bool) string {
	if focus == nil {
		return "<missing focus>\n"
	}
	var sb strings.Builder
	writeListedEntry(&sb, *focus, brief)
	// The entry line writes "  <id> ... \n"; strip the leading spaces — the
	// `### ` marker provides visual offset already.
	return strings.TrimLeft(sb.String(), " ")
}

// writeFocusDefaults emits the focus-level when/actors lines under the
// header so per-target lines that inherit don't need to repeat them.
// Lines are omitted when the corresponding default is unset.
func writeFocusDefaults(w io.Writer, focus types.ViewFocus) {
	if !focus.When.IsZero() {
		fmt.Fprintf(w, "    when: %s\n", formatFocusWhen(focus.When))
	}
	if len(focus.Actors) > 0 {
		fmt.Fprintf(w, "    actors: %s\n", strings.Join(focus.Actors, ", "))
	}
}

// writeFocusTarget renders one involvement target with state + score
// segments. Indented four spaces under the focus header so the visual
// nesting is unambiguous when several focuses sit in the same block.
func writeFocusTarget(w io.Writer, target types.ViewFocusTarget, brief bool) {
	fmt.Fprint(w, "  - ")
	fmt.Fprintf(w, "{state: %s} ", target.State)
	// Inline a trimmed scored entry line — same shape as the as-list
	// scored output so readers can compare across surfaces. The leading
	// two-space indent of the entry line collapses with our `  - ` prefix.
	// Brief keeps the state segment (it is what the block is for) and
	// compacts the entry line itself.
	var sb strings.Builder
	if brief {
		writeEntryLineBrief(&sb, target.Target)
	} else {
		writeEntryLine(&sb, target.Target, target.Score)
	}
	fmt.Fprint(w, strings.TrimLeft(sb.String(), " "))
}

// formatFocusWhen renders a FocusWhen as `<from> → <to>`, omitting the
// missing end (`<from> →` or `→ <to>`). Used in the focus header.
func formatFocusWhen(when *types.FocusWhen) string {
	switch {
	case when.From != "" && when.To != "":
		return when.From + " → " + when.To
	case when.From != "":
		return when.From + " →"
	case when.To != "":
		return "→ " + when.To
	default:
		return ""
	}
}
