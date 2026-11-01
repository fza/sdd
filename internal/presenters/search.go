package presenters

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/networkteam/sdd/pkg/application/types"
)

// RenderSearch writes one section per ranked hit (query.SearchResult.Hits): a
// header line in the shared entry-line shape, then one citation line per
// citation (capped by SearchQuery.MaxCitationsPerEntry on the finder side).
//
// Render format (single-citation case):
//
//	<id> <layer> <kind>? <type> [confidence: ?]? (<participants>) {status: ?}? <summary>
//	  ↳ 100%  ·  Breadcrumb > Chain  ·  <snippet>
//
// Render format (multi-citation case):
//
//	<id> ...
//	  ↳ 100%  ·  Summary  ·  <snippet>
//	  ↳  91%  ·  Approach > Storage  ·  <snippet>
//	  ↳  87%  ·  ... [attachment: ...]  ·  <snippet>
//
// The percentage is each citation's score normalized against the
// strongest score in the result set — the top citation everywhere
// renders 100%, others scale relative. This is honest about the cross-
// entry ranking without making any claims about absolute "confidence"
// (cosine values aren't calibrated across embedders, and text-mode
// match counts and hybrid RRF scores live on different scales than
// cosine — but ALL citations within a single result set share whatever
// scale the active mode uses).
func RenderSearch(w io.Writer, hits []types.SearchHit) {
	if len(hits) == 0 {
		fmt.Fprintln(w, "  (no matches)")
		return
	}

	maxScore := globalMaxCitationScore(hits)
	for _, hit := range hits {
		writeEntryLine(w, hitEntry(hit), math.NaN())
		for _, c := range hit.Citations {
			renderCitation(w, c, maxScore)
		}
	}
}

// SearchText is the search text of the application and the MCP server:
// RenderSearch without the trailing newline, each notice following as its own
// paragraph.
func SearchText(hits []types.SearchHit, notices []string) string {
	var rendered strings.Builder
	RenderSearch(&rendered, hits)
	text := strings.TrimRight(rendered.String(), "\n")
	for _, notice := range notices {
		if text != "" {
			text += "\n\n"
		}
		text += notice
	}
	return text
}

// FormatSearchCapability renders the capability suffix shown in
// `sdd info`'s header: `text` when only text mode is available;
// `vector,text` when a vector embedder is configured. Used by callers
// that want to surface the search capability alongside other header fields.
func FormatSearchCapability(vectorAvailable bool) string {
	if vectorAvailable {
		return "vector,text"
	}
	return "text"
}

// globalMaxCitationScore returns the largest score across every
// citation in the result. Used to normalize per-citation scores into a
// relative percentage at render time. Returns 0 when the result is
// empty (the caller short-circuits this case before calling, but the
// guard keeps the helper total).
func globalMaxCitationScore(hits []types.SearchHit) float32 {
	var max float32
	for _, hit := range hits {
		for _, c := range hit.Citations {
			if c.Score > max {
				max = c.Score
			}
		}
	}
	return max
}

func renderCitation(w io.Writer, c types.SearchCitation, maxScore float32) {
	var b strings.Builder
	b.WriteString("    ↳ ")
	b.WriteString(formatRelativeScore(c.Score, maxScore))
	b.WriteString("  ·  ")
	if c.IsSummary {
		b.WriteString("Summary")
	} else if len(c.Breadcrumb) > 0 {
		b.WriteString(strings.Join(c.Breadcrumb, " > "))
	} else {
		b.WriteString("Body")
	}
	if c.IsAttachment && c.AttachmentPath != "" {
		b.WriteString(" [attachment: ")
		b.WriteString(c.AttachmentPath)
		b.WriteString("]")
	}
	if c.Snippet != "" {
		b.WriteString("  ·  ")
		b.WriteString(c.Snippet)
	}
	b.WriteString("\n")
	fmt.Fprint(w, b.String())
}

// formatRelativeScore formats a citation's score as a percentage of the
// result-set max, right-padded to 4 chars so the citation lines align
// vertically (`100%`, ` 91%`, ` 5%` all occupy the same width). When
// max is zero (no scores recorded), renders blank padding so the
// pipeline doesn't break.
func formatRelativeScore(score, max float32) string {
	if max <= 0 {
		return "    "
	}
	pct := int((score / max) * 100)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return fmt.Sprintf("%3d%%", pct)
}
