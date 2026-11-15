package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/pkg/application/types"
)

// renderAsParticipantsBlock writes one `### <canonical>` per active
// actor head, with the actor's entry line followed by every derived-
// active role bound to that chain — the Participants block surfaced by
// `sdd view --layout='participants'`.
//
// Empty groups suppress output entirely. The CLI shell handles the
// outer `## <name>` header (when name() is set), so this function
// focuses on the per-actor bodies.
func renderAsParticipantsBlock(w io.Writer, block types.ViewParticipantsBlock, brief bool) {
	if len(block.Groups) == 0 {
		return
	}
	for i, grp := range block.Groups {
		if grp.Actor == nil {
			continue
		}
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "### %s\n", grp.Actor.Canonical)
		writeListedEntry(w, *grp.Actor, brief)
		for _, r := range grp.Roles {
			writeListedEntry(w, r, brief)
		}
	}
	if block.Dropped > 0 {
		fmt.Fprintln(w)
		writeSectionCut(w, block.Dropped, block.Pull)
	}
}
