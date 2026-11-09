package presenters

import (
	"fmt"
	"io"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// RenderRecheckItems renders the unhealed recheck items as the catch-up's
// recheck section with its healing suggestion (20261109-004506-d-cpt-udz,
// D5); nothing when no item is open.
func RenderRecheckItems(w io.Writer, g *model.Graph, items []model.RecheckItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(w, "## Recheck (%d open)\n\n", len(items))
	for _, item := range items {
		briefs := make([]string, 0, len(item.Entries))
		for _, id := range item.Entries {
			brief := id
			if e := g.ByID[id]; e != nil {
				brief = id + " " + model.EntryBrief(e)
			}
			briefs = append(briefs, brief)
		}
		switch item.Kind {
		case model.RecheckStrain:
			fmt.Fprintf(w, "- strain, %s: %s", item.Relation, strings.Join(briefs, " / "))
			if item.ReportedBy != "" {
				fmt.Fprintf(w, " (reported by %s)", item.ReportedBy)
			}
			fmt.Fprintln(w)
		case model.RecheckHandEdit:
			fmt.Fprintf(w, "- hand edit in commit %s: %s\n", shortCommit(item.Commit), strings.Join(briefs, " / "))
		}
	}
	fmt.Fprintln(w, "\nSuggest a healing run: for each item, a capture that cites every entry of the item heals it (a new entry that resolves the clash or restates the edited entry); an entry superseded or closed leaves the list too.")
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
