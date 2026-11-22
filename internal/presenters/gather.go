package presenters

import (
	"fmt"
	"io"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// GatherWayBack is the call a pointer names to have an entry served in full
// again.
const GatherWayBack = "show with its id"

// GatherInstruction is the duty a graph check states, word for word
// (20261121-223938-d-tac-rqi).
const GatherInstruction = "Open every entry you would reference and every entry that could contradict the draft. Then open those that look like bearing on the topic."

// RenderGather writes a graph check: the header, the instruction, the
// entries opened by ID in full, the lines likely bearing, in doubt and by
// title, and one count per set-aside reason. Nothing is cut: every line
// prints whole.
func RenderGather(w io.Writer, result *query.GatherResult) error {
	return renderGather(w, result, &Repeats{Held: result.Held, Served: result.Served})
}

func renderGather(w io.Writer, result *query.GatherResult, repeats *Repeats) error {
	g := result.Graph
	lines := result.Lines
	setAside := 0
	for _, group := range result.SetAside {
		setAside += len(group.IDs)
	}
	fmt.Fprintf(w, "# graph check   %d candidates · judged %d of %d · %d shown · %d set aside\n", result.Found, result.Judged, result.Found, len(lines.Bearing)+len(lines.Doubt), setAside)
	fmt.Fprintln(w)
	var reads []string
	for _, read := range result.Reads {
		if read.Mode != model.GroundingOpened {
			reads = append(reads, fmt.Sprintf("%s %d", gatherReadLabel(read), len(read.IDs)))
		}
	}
	if len(reads) > 0 {
		fmt.Fprintf(w, "reads: %s\n", strings.Join(reads, " · "))
	}
	if result.Corpus != nil {
		fmt.Fprintf(w, "corpus: %s live, mark %s\n", entryCount(result.Corpus.Count), shortHash(result.Corpus.Hash))
	}
	prefilter := result.Prefilter
	if prefilter == "" {
		prefilter = model.PrefilterOff
	}
	if result.PrefilterNote != "" {
		prefilter += ": " + result.PrefilterNote
	}
	fmt.Fprintf(w, "prefilter: %s\n", prefilter)
	fmt.Fprintln(w)
	fmt.Fprintln(w, GatherInstruction+" Open entries with gather ids, several in one call.")

	wayBackShown := false
	if result.Opened != nil && len(result.Opened.Groups) > 0 {
		entries, err := result.Opened.ShownEntries()
		if err != nil {
			return err
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "## opened (%d)\n", len(entries))
		fmt.Fprintln(w)
		err = RenderShow(w, entries, ShowOptions{Repeats: repeats})
		if err != nil {
			return err
		}
		wayBackShown = repeats.Pointed()
	}

	if len(lines.Bearing) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "## likely bearing (%d) — each owes a verdict or a reference\n", len(lines.Bearing))
		for _, line := range lines.Bearing {
			line.Summary = true
			writeMarkedLine(w, g, line, repeats)
		}
	}
	if len(lines.Doubt) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "## in doubt (%d)\n", len(lines.Doubt))
		for _, line := range lines.Doubt {
			writeMarkedLine(w, g, line, repeats)
		}
	}
	if len(lines.Titles) > 0 || len(lines.Counted) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "## by title (%d)\n", len(lines.Titles))
		for _, line := range lines.Titles {
			fmt.Fprintf(w, "  %s  %s\n", line.Entry.ID, line.Entry.ListingTitle())
		}
		if len(lines.Counted) > 0 {
			fmt.Fprintf(w, "  +%d more, not listed\n", len(lines.Counted))
		}
	}
	if !wayBackShown {
		repeats.WriteWayBack(w)
	}

	if setAside > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "## set aside (%d)\n", setAside)
		for _, group := range result.SetAside {
			fmt.Fprintf(w, "  %d %s\n", len(group.IDs), group.Reason)
		}
	}
	return nil
}

// writeMarkedLine writes one line: its marks, ID, status and title, a newer
// refinement, and the first summary sentence below it when the line carries
// one. A held entry prints its pointer in place of ID, status and title.
func writeMarkedLine(w io.Writer, g *model.Graph, line model.Line, repeats *Repeats) {
	e := line.Entry
	if pointer, held := repeats.pointer(e.ID, model.EntryBrief(e)); held {
		fmt.Fprintf(w, "%s  %s\n", LineMarks(line), pointer)
		return
	}
	fmt.Fprint(w, MarkedLine(g, line, ""))
}

// MarkedLine renders one line of a reading: its marks, ID, status and title,
// a newer refinement, the note after a dash, and the first summary sentence
// on a line of its own when the line carries one.
func MarkedLine(g *model.Graph, line model.Line, note string) string {
	e := line.Entry
	var b strings.Builder
	b.WriteString(LineMarks(line) + "  " + e.ID)
	if status := lineStatus(g.DerivedStatus(e)); status != "" {
		b.WriteString(" " + status)
	}
	b.WriteString("  " + e.ListingTitle())
	if refiner := model.NewerRefiner(g, e.ID); refiner != "" {
		fmt.Fprintf(&b, " → refined by %s (newer)", refiner)
	}
	if note != "" {
		b.WriteString(" — " + note)
	}
	b.WriteString("\n")
	if sentence := e.FirstSummarySentence(); line.Summary && e.Title != "" && sentence != "" {
		b.WriteString("      " + sentence + "\n")
	}
	return b.String()
}

// LineMarks is the mark column of a line: `!` for likely bearing, the
// judge's score or `?` for not judged, and `~ref` for one reference step
// from an entry the draft references.
func LineMarks(line model.Line) string {
	mark := " "
	if line.Marked {
		mark = "!"
	}
	score := "?  "
	if line.Score != nil {
		score = LineScore(*line.Score)
	}
	hop := "    "
	if line.Hop {
		hop = "~ref"
	}
	return mark + " " + score + " " + hop
}

// LineScore writes a judge's score in two decimals without the leading zero.
func LineScore(score float64) string {
	text := fmt.Sprintf("%.2f", score)
	if strings.HasPrefix(text, "0.") {
		return text[1:]
	}
	return "1.0"
}

func lineStatus(s model.Status) string {
	if s.Kind == model.StatusNone {
		return ""
	}
	if s.By != "" {
		return "{" + string(s.Kind) + " " + s.By + "}"
	}
	return "{" + string(s.Kind) + "}"
}

// topicPathLabels is each topic path in its label form.
func topicPathLabels(paths []model.TopicPath) []string {
	labels := make([]string, 0, len(paths))
	for _, path := range paths {
		labels = append(labels, path.String())
	}
	return labels
}

// RenderLateEntries writes the entries that landed after the assemble check
// as brief lines, one each, nothing cut; an entry held reports in full
// prints as its pointer.
func RenderLateEntries(w io.Writer, g *model.Graph, late []*model.Entry, repeats *Repeats) {
	for _, e := range late {
		fmt.Fprintln(w, gatherLine(g, e, repeats))
	}
	repeats.WriteWayBack(w)
}

// GatherPointer is the pointer a held entry prints as.
func GatherPointer(e *model.Entry) string {
	return pointerText(e.ID, model.EntryBrief(e))
}

func gatherLine(g *model.Graph, e *model.Entry, r *Repeats) string {
	var line strings.Builder
	line.WriteString("- ")
	if pointer, held := r.pointer(e.ID, model.EntryBrief(e)); held {
		line.WriteString(pointer)
		return line.String()
	}
	fmt.Fprintf(&line, "%s (%s %s %s)", e.ID, e.Kind, e.LayerLabel(), e.TypeLabel())
	if status := FormatStatus(g.DerivedStatus(e)); status != "" {
		line.WriteString(" ")
		line.WriteString(status)
	}
	line.WriteString(" — ")
	line.WriteString(model.EntryBrief(e))
	if topics := formatTopics(topicPathLabels(g.EffectiveTopics(e))); topics != "" {
		line.WriteString(" ")
		line.WriteString(topics)
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
