package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/pkg/application/types"
)

// renderAsGrouped writes one `### <key>` header per group followed by
// the group's entry lines. Groups are separated by a blank line so the
// visual cluster is obvious; a trailing blank line is omitted to match
// renderAsList's "no trailing newline beyond the last entry" shape.
//
// The section as a whole carries no `## <title>` header — that's the
// `name(...)` modifier's job, landing in slice 6. Slice 5 keeps the
// presenter pure: groups in, headers + entry lines out.
func renderAsGrouped(w io.Writer, grouped types.ViewGrouped, brief bool, r *Repeats) {
	for i, group := range grouped.Groups {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "### %s\n", group.Key)
		for _, e := range group.Entries {
			r.entry(w, e.ID, e.Brief, func() { writeListedEntry(w, e, brief) })
		}
	}
}
