package presenters_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// bodiesSection wraps entries in the section shape as-bodies consumes, named
// like a framing lane so the nesting under a `## <name>` header is exercised.
func bodiesSection(name string, entries ...*model.Entry) *query.ViewResult {
	return &query.ViewResult{
		Graph: model.NewGraph(entries),
		Sections: []query.SectionResult{{
			Render: "as-bodies",
			Name:   name,
			Data:   model.Bodies{Entries: entries},
		}},
	}
}

func TestRenderView_AsBodies_LeadingHeadingBecomesTitle(t *testing.T) {
	e := entry("20260101-100000-s-prc-aaa",
		withKind(model.KindFact),
		withSummary("summary that must not be served alongside the body"),
		withContent("# The posture\n\nGoal first.\n\n## Misfit\n\nMisfit is a signal.\n"))

	got := renderView(bodiesSection("Working principles", e))
	want := "## Working principles\n\n" +
		"### The posture\n\n" +
		"`20260101-100000-s-prc-aaa` — process fact signal\n\n" +
		"Goal first.\n\n" +
		"#### Misfit\n\n" +
		"Misfit is a signal.\n"
	if got != want {
		t.Errorf("as-bodies render mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "summary that must not be served") {
		t.Error("the identity header must not carry the summary — it derives from the body already served")
	}
}

func TestRenderView_AsBodies_HeadinglessBodyTakesIdentityAsHeader(t *testing.T) {
	e := entry("20260101-100000-s-prc-aaa",
		withKind(model.KindFact),
		withContent("Opening prose with no heading.\n\n## Section\n\nunder it\n"))

	got := renderView(bodiesSection("Working principles", e))
	want := "## Working principles\n\n" +
		"### `20260101-100000-s-prc-aaa` — process fact signal\n\n" +
		"Opening prose with no heading.\n\n" +
		"#### Section\n\n" +
		"under it\n"
	if got != want {
		t.Errorf("as-bodies render mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestRenderView_AsBodies_NoBodyHeadingOutranksTheStructure is the invariant
// itself (d-cpt-5wv): whatever levels a body was authored at, nothing it
// contributes may sit at or above the lane and entry headers serving it.
func TestRenderView_AsBodies_NoBodyHeadingOutranksTheStructure(t *testing.T) {
	e := entry("20260101-100000-s-prc-aaa",
		withKind(model.KindFact),
		withContent("# Top\n\n## Mid\n\n### Deep\n\n```sh\n# a comment, not a heading\n```\n"))

	got := renderView(bodiesSection("Working principles", e))
	inFence := false
	for line := range strings.SplitSeq(got, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(line, "#") {
			continue
		}
		level := len(line) - len(strings.TrimLeft(line, "#"))
		switch {
		case strings.HasPrefix(line, "## Working principles"), strings.HasPrefix(line, "### Top"):
		case level <= 3:
			t.Errorf("body heading %q outranks the entry header it belongs under", line)
		}
	}
	if !strings.Contains(got, "# a comment, not a heading") || strings.Contains(got, "#### a comment") {
		t.Errorf("fenced content must survive verbatim, got:\n%s", got)
	}
}

func TestRenderView_AsBodies_UnnamedSectionNestsOneLevelHigher(t *testing.T) {
	e := entry("20260101-100000-s-prc-aaa",
		withKind(model.KindFact),
		withContent("# The posture\n\nGoal first.\n"))

	got := renderView(bodiesSection("", e))
	want := "## The posture\n\n`20260101-100000-s-prc-aaa` — process fact signal\n\nGoal first.\n"
	if got != want {
		t.Errorf("without a section header the entry header is the top level:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderView_AsBodies_EmptySelectionServesNothing(t *testing.T) {
	if got := renderView(bodiesSection("Working principles")); got != "" {
		t.Errorf("an empty selection must serve no block at all, got %q", got)
	}
}

func TestRenderView_AsBodies_MultipleEntriesInSectionOrder(t *testing.T) {
	first := entry("20260101-100000-s-prc-aaa",
		withKind(model.KindFact),
		withContent("# First\n\none\n"))
	second := entry("20260101-110000-s-prc-bbb",
		withKind(model.KindFact),
		withContent("# Second\n\ntwo\n"))

	got := renderView(bodiesSection("Working principles", first, second))
	if i, j := strings.Index(got, "### First"), strings.Index(got, "### Second"); i < 0 || j < 0 || i > j {
		t.Errorf("entries render in the order the section produced them, got:\n%s", got)
	}
	if !strings.Contains(got, "one\n\n### Second") {
		t.Errorf("entries are separated by a blank line, got:\n%s", got)
	}
}

// TestRenderView_AsBodies_WithheldBodiesAreNamed pins the honest whole-body
// cut (d-tac-rzi): a body the serve budget withheld leaves the section naming
// the entry and the pull, even when nothing of the section fits.
func TestRenderView_AsBodies_WithheldBodiesAreNamed(t *testing.T) {
	kept := entry("20260101-100000-s-prc-aaa", withKind(model.KindFact), withContent("# Kept\n\nServed whole.\n"))
	withheld := entry("20260101-110000-s-prc-bbb", withKind(model.KindFact),
		withSummary("The long posture this project works by."), withContent("# Withheld\n\nNever served in part.\n"))
	const pull = `topic("principles/interactive"):as-bodies:name("Working principles")`
	notice := "(withheld for size, 1 whole — pull: " + pull + ")\n" +
		"- 20260101-110000-s-prc-bbb — The long posture this project works by.\n"

	cases := []struct {
		name   string
		bodies model.Bodies
		want   string
	}{
		{
			name:   "section cut to nothing",
			bodies: model.Bodies{Withheld: []*model.Entry{withheld}, Pull: pull},
			want:   "## Working principles\n\n" + notice,
		},
		{
			name:   "section cut after a kept body",
			bodies: model.Bodies{Entries: []*model.Entry{kept}, Withheld: []*model.Entry{withheld}, Pull: pull},
			want: "## Working principles\n\n" +
				"### Kept\n\n`20260101-100000-s-prc-aaa` — process fact signal\n\nServed whole.\n\n" + notice,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderView(&query.ViewResult{
				Graph:    model.NewGraph([]*model.Entry{kept, withheld}),
				Sections: []query.SectionResult{{Render: "as-bodies", Name: "Working principles", Data: tc.bodies}},
			})
			if got != tc.want {
				t.Errorf("withheld render mismatch:\ngot:\n%s\nwant:\n%s", got, tc.want)
			}
			if strings.Contains(got, "Never served in part") {
				t.Error("a withheld body must not serve any of its text")
			}
		})
	}
}
