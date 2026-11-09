package presenters

import (
	"fmt"
	"io"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// GatherWayBack is the call a pointer names to have an entry served in full
// again (D34).
const GatherWayBack = "gather with its ids, or resume_session"

// RenderGather writes a gather serve: header, the entries served in full,
// the candidates to judge and the entries set aside. Nothing is cut (D29):
// every list prints whole, and what the full block leaves out is named with
// the call that opens it.
func RenderGather(w io.Writer, result *query.GatherResult) {
	g := result.Graph
	fmt.Fprintln(w, "# gather")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "reads:")
	for _, read := range result.Reads {
		fmt.Fprintf(w, "- %s: %s\n", gatherReadLabel(read), entryCount(len(read.IDs)))
	}
	if result.Corpus != nil {
		fmt.Fprintf(w, "corpus: %s live, mark %s\n", entryCount(result.Corpus.Count), shortHash(result.Corpus.Hash))
	}
	prefilter := result.Prefilter
	if prefilter == "" {
		prefilter = "off"
	}
	fmt.Fprintf(w, "prefilter: %s\n", prefilter)

	if result.Full != nil && len(result.Full.Groups) > 0 || len(result.Overflow) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "# relevant")
		if result.Full != nil && len(result.Full.Groups) > 0 {
			fmt.Fprintln(w)
			RenderShow(w, result.Full, ShowOptions{})
		}
		if len(result.Overflow) > 0 {
			ids := make([]string, len(result.Overflow))
			for i, e := range result.Overflow {
				ids[i] = e.ID
			}
			fmt.Fprintln(w)
			fmt.Fprintf(w, "+%d opened entries past the full-serve cap of %d, open them with gather ids [%s]:\n", len(ids), query.GatherFullCap, strings.Join(ids, ", "))
			for _, e := range result.Overflow {
				fmt.Fprintln(w, gatherLine(g, model.GatherCandidate{Entry: e}, false))
			}
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "# to judge (%d)\n", len(result.ToJudge))
	if len(result.ToJudge) > 0 {
		fmt.Fprintln(w)
		held := false
		for _, candidate := range result.ToJudge {
			isHeld := result.Held[candidate.Entry.ID]
			held = held || isHeld
			fmt.Fprintln(w, gatherLine(g, candidate, isHeld))
		}
		if held {
			fmt.Fprintln(w)
			fmt.Fprintf(w, "An entry marked served in full is not served again; to have it served in full, call %s.\n", GatherWayBack)
		}
	}

	if len(result.SetAside) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "# set aside")
		fmt.Fprintln(w)
		for _, group := range result.SetAside {
			fmt.Fprintf(w, "- %s: %d: %s\n", group.Reason, len(group.IDs), strings.Join(group.IDs, ", "))
		}
	}
}

// RenderLateEntries writes the entries that landed after the assemble check
// as brief lines, one each, nothing cut (D37).
func RenderLateEntries(w io.Writer, g *model.Graph, late []*model.Entry) {
	for _, e := range late {
		fmt.Fprintln(w, gatherLine(g, model.GatherCandidate{Entry: e}, false))
	}
}

// GatherPointer is the pointer an already-served entry prints as: its ID, its
// brief, never cut, and a marker naming the way back.
func GatherPointer(e *model.Entry) string {
	return fmt.Sprintf("%s — %s (served in full above)", e.ID, model.EntryBrief(e))
}

func gatherLine(g *model.Graph, candidate model.GatherCandidate, held bool) string {
	e := candidate.Entry
	var line strings.Builder
	line.WriteString("- ")
	if held {
		line.WriteString(GatherPointer(e))
	} else {
		fmt.Fprintf(&line, "%s (%s %s %s)", e.ID, e.Kind, e.LayerLabel(), e.TypeLabel())
		if status := FormatStatus(g.DerivedStatus(e)); status != "" {
			line.WriteString(" ")
			line.WriteString(status)
		}
		line.WriteString(" — ")
		line.WriteString(model.EntryBrief(e))
		if topics := FormatTopics(g.EffectiveTopics(e)); topics != "" {
			line.WriteString(" ")
			line.WriteString(topics)
		}
	}
	if len(candidate.FoundBy) > 0 {
		line.WriteString(" · found by ")
		line.WriteString(strings.Join(candidate.FoundBy, ", "))
	}
	return line.String()
}

func gatherReadLabel(read model.GroundingRead) string {
	switch read.Mode {
	case model.GroundingSemantic:
		return fmt.Sprintf("semantic %q", read.Text)
	case model.GroundingKeyword:
		return fmt.Sprintf("keyword %q", read.Text)
	case model.GroundingListing:
		return fmt.Sprintf("listing topic(%s)", read.Text)
	case model.GroundingBriefs:
		return "every live brief, no search"
	case model.GroundingOpened:
		return "opened " + strings.Join(read.IDs, ", ")
	case model.GroundingNew:
		return "new since the reading began"
	}
	return string(read.Mode)
}

func entryCount(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
