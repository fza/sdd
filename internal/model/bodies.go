package model

// ShapeBodies marks results carrying entries to be rendered as their full
// bodies — produced by a section ending in `as-bodies`. It is its own shape
// rather than a flat list because a body render consumes no per-row
// decoration: ref sub-lines and compact entry lines have nothing to attach
// to once the body itself is the output.
const ShapeBodies RenderShape = "bodies"

// Bodies is the result shape consumed by the as-bodies render. Selection,
// ordering, and capping are the pipeline's ordinary business, so the shape
// carries nothing but the entries in the order the section produced them.
type Bodies struct {
	Entries []*Entry
	// Withheld are the whole bodies a serve budget kept out of this section,
	// named so the cut says what it dropped; Pull is the runnable layout
	// expression for the complete section. Empty on explicit pulls, which are
	// never cut (d-tac-rzi).
	Withheld []*Entry
	Pull     string
}

// Shape implements SectionData.
func (Bodies) Shape() RenderShape { return ShapeBodies }

// Count implements SectionData. Withheld bodies count: the section matched
// them, and a section its budget cut to nothing still renders that cut.
func (b Bodies) Count() int { return len(b.Entries) + len(b.Withheld) }
