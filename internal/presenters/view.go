package presenters

import (
	"fmt"
	"io"
	"strings"

	"github.com/networkteam/sdd/pkg/application/types"
)

// RenderView writes the view sections (query.ViewResult.ViewSections),
// dispatching each to the per-render helper its Render names. Sections are
// separated by a blank line so visual clusters are obvious. A non-empty Name
// renders as a `## <name>` header before the section body; empty Name omits
// the header (the as-list / as-grouped default). Each run of sections from one
// connected repo opens with a `── repo: <id> ──` heading; entry IDs inside it
// are scoped by that heading.
//
// A section that matched nothing renders nothing at all, header included: a
// title with no content under it claims a structure the result does not have,
// and surfaces that need to say "matched nothing" say it once for the whole
// result rather than once per empty section. The exception is a render whose
// empty state is itself the message (see rendersWhenEmpty).
//
// Render names are matched against the known set; an unknown name, or a
// section whose filled shape is not the one its render reads, produces no body
// for that section. The finder validates the pipeline shape before reaching
// here, so either implies a programmer-side mistake (a layout produced by some
// path other than the parser+executor) rather than a user-facing error.
//
// r applies the no-repeat rule; nil serves everything in full.
func RenderView(w io.Writer, sections []types.ViewSection, r *Repeats) {
	written := 0
	repo := ""
	for _, section := range sections {
		if section.Repo != repo {
			repo = section.Repo
			fmt.Fprintf(w, "\n── repo: %s ──\n", repo)
			written = 0
		}
		if section.Matched == 0 && !rendersWhenEmpty[section.Render] {
			continue
		}
		if written > 0 {
			fmt.Fprintln(w)
		}
		if section.Name != "" {
			fmt.Fprintf(w, "## %s\n\n", section.Name)
		}
		renderSection(w, section, r)
		written++
	}
	r.WriteWayBack(w)
}

// ViewText is the view text of the application, the engine and the MCP
// server: RenderView without the trailing newline.
func ViewText(sections []types.ViewSection, r *Repeats) string {
	var rendered strings.Builder
	RenderView(&rendered, sections, r)
	return strings.TrimRight(rendered.String(), "\n")
}

// sectionHeadingLevel is the level RenderView writes a named section's header
// at; embedded content nests below it.
const sectionHeadingLevel = 2

// rendersWhenEmpty names the renders whose empty state is itself the message —
// as-wip-list reports that no work is in flight, which is what a reader of that
// section wanted to know. Every other render contributes nothing when it matched
// nothing, so its section is dropped whole.
var rendersWhenEmpty = map[string]bool{"as-wip-list": true}

func renderSection(w io.Writer, section types.ViewSection, r *Repeats) {
	switch section.Render {
	case "as-bodies":
		if section.Bodies == nil {
			return
		}
		entryLevel := sectionHeadingLevel + 1
		if section.Name == "" {
			entryLevel = sectionHeadingLevel
		}
		renderAsBodies(w, *section.Bodies, entryLevel, r)
	case "as-list":
		if section.List == nil {
			return
		}
		renderAsList(w, *section.List, section.Brief, r)
	case "as-grouped":
		if section.Grouped == nil {
			return
		}
		renderAsGrouped(w, *section.Grouped, section.Brief, r)
	case "as-counts":
		if section.Counts == nil {
			return
		}
		renderAsCounts(w, *section.Counts)
	case "as-focus-block":
		if section.Focus == nil {
			return
		}
		renderAsFocusBlock(w, *section.Focus, section.Brief, r)
	case "as-participants-block":
		if section.Participants == nil {
			return
		}
		renderAsParticipantsBlock(w, *section.Participants, section.Brief, r)
	case "as-wip-list":
		if section.Wip == nil {
			return
		}
		renderAsWipList(w, *section.Wip)
	}
}
