package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/internal/model"
)

// RenderAgentChains renders the catch-up's lane of agent reasoning awaiting a
// person's review: each chain's root with the entries resting on it; nothing
// when no chain waits.
func RenderAgentChains(w io.Writer, chains []model.AgentChain) {
	if len(chains) == 0 {
		return
	}
	fmt.Fprintf(w, "## Agent-decided, awaiting review (%d)\n\n", len(chains))
	for _, chain := range chains {
		fmt.Fprintf(w, "- %s %s: %s\n", chain.Root.ID, kindLabel(chain.Root), model.EntryBrief(chain.Root))
		for _, e := range chain.Resting {
			fmt.Fprintf(w, "  - rests on it: %s %s: %s\n", e.ID, kindLabel(e), model.EntryBrief(e))
		}
	}
	fmt.Fprintln(w, "\nA person reviews each chain in a guided session: a settled directive listing the entries under approves clears them, one closing them dismisses them.")
}

func kindLabel(e *model.Entry) string {
	if e.Kind == "" {
		return string(e.Layer) + " " + string(e.Type)
	}
	return string(e.Layer) + " " + string(e.Kind)
}
