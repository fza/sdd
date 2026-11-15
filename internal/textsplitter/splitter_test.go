package textsplitter

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSplitter_StripsFrontmatter covers d-tac-85r's "Strip YAML frontmatter
// at the package entry point; surface frontmatter values to the caller as
// filterable metadata, not embedded text."
func TestSplitter_StripsFrontmatter(t *testing.T) {
	t.Parallel()

	input := `---
type: decision
layer: tactical
kind: plan
participants:
  - Christopher
  - Claude
---
## Section
Body text under section.
`

	got, err := NewSplitter().Split(SplitInput{
		Markdown:     input,
		EntrySummary: "Plan summary first sentence. Second sentence is irrelevant.",
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	wantFM := map[string]any{
		"type":         "decision",
		"layer":        "tactical",
		"kind":         "plan",
		"participants": []any{"Christopher", "Claude"},
	}
	if !reflect.DeepEqual(got.Frontmatter, wantFM) {
		t.Errorf("frontmatter:\n  got  %#v\n  want %#v", got.Frontmatter, wantFM)
	}

	if len(got.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %#v", len(got.Chunks), got.Chunks)
	}

	// Body excludes frontmatter.
	if strings.Contains(got.Chunks[0].Body, "type: decision") {
		t.Errorf("frontmatter leaked into chunk body: %q", got.Chunks[0].Body)
	}

	// Text carries Entry: + Breadcrumb: + body, with the summary's first
	// sentence (per d-tac-jvd) and the heading chain.
	want := "Entry: Plan summary first sentence.\nBreadcrumb: Section\n\nBody text under section."
	if got.Chunks[0].Text != want {
		t.Errorf("text:\n  got  %q\n  want %q", got.Chunks[0].Text, want)
	}
}

// TestSplitter_NoFrontmatter confirms documents without frontmatter pass
// through unchanged.
func TestSplitter_NoFrontmatter(t *testing.T) {
	t.Parallel()

	got, err := NewSplitter().Split(SplitInput{
		Markdown:     "## Heading\nbody",
		EntrySummary: "Summary.",
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if got.Frontmatter != nil {
		t.Errorf("expected nil frontmatter, got %#v", got.Frontmatter)
	}
	if len(got.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got.Chunks))
	}
	if got.Chunks[0].Text != "Entry: Summary.\nBreadcrumb: Heading\n\nbody" {
		t.Errorf("text: got %q", got.Chunks[0].Text)
	}
}

// TestSplitter_PreambleOmitsEmptyFields covers the composePreamble shape
// when summary or breadcrumb are empty.
func TestSplitter_PreambleOmitsEmptyFields(t *testing.T) {
	t.Parallel()

	t.Run("no-summary", func(t *testing.T) {
		t.Parallel()
		got, err := NewSplitter().Split(SplitInput{Markdown: "## H\nbody", EntrySummary: ""})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Chunks) != 1 {
			t.Fatalf("got %d chunks", len(got.Chunks))
		}
		if got.Chunks[0].Text != "Breadcrumb: H\n\nbody" {
			t.Errorf("got %q", got.Chunks[0].Text)
		}
	})

	t.Run("no-breadcrumb", func(t *testing.T) {
		t.Parallel()
		got, err := NewSplitter().Split(SplitInput{Markdown: "Just body, no headings.", EntrySummary: "S."})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Chunks) != 1 {
			t.Fatalf("got %d chunks", len(got.Chunks))
		}
		if got.Chunks[0].Text != "Entry: S.\n\nJust body, no headings." {
			t.Errorf("got %q", got.Chunks[0].Text)
		}
	})

	t.Run("neither", func(t *testing.T) {
		t.Parallel()
		got, err := NewSplitter().Split(SplitInput{Markdown: "Just body."})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Chunks) != 1 {
			t.Fatalf("got %d chunks", len(got.Chunks))
		}
		// No preamble at all when both are empty.
		if got.Chunks[0].Text != "Just body." {
			t.Errorf("got %q", got.Chunks[0].Text)
		}
	})
}

// TestSplitter_AttachmentMetadata covers the AC "Attachments referenced by
// an entry are chunked and embedded under the parent entry's ID; chunk
// metadata records the source attachment path." (Stamping is the
// splitter's contribution; the indexer ties chunks to the parent entry's
// id.)
func TestSplitter_AttachmentMetadata(t *testing.T) {
	t.Parallel()

	got, err := NewSplitter().Split(SplitInput{
		Markdown:             "# Design\nDesign details.",
		EntrySummary:         "Plan summary.",
		IsAttachment:         true,
		SourceAttachmentPath: "2026/05/04-235258-d-tac-lqr/design.md",
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	if len(got.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got.Chunks))
	}
	c := got.Chunks[0]
	if !c.IsAttachment {
		t.Error("IsAttachment should be true")
	}
	if c.IsSummary {
		t.Error("IsSummary should be false on body chunk")
	}
	if c.SourceAttachmentPath != "2026/05/04-235258-d-tac-lqr/design.md" {
		t.Errorf("SourceAttachmentPath: got %q", c.SourceAttachmentPath)
	}
}

// TestSplitter_SummaryChunk verifies the dedicated summary chunk: text is
// the raw summary (no preamble), IsSummary is true, empty input returns
// (zero, false).
func TestSplitter_SummaryChunks(t *testing.T) {
	t.Parallel()

	s := NewSplitter()

	t.Run("populated", func(t *testing.T) {
		t.Parallel()
		cs := s.SummaryChunks("This is the summary.")
		if len(cs) != 1 {
			t.Fatalf("chunks: got %d, want 1", len(cs))
		}
		c := cs[0]
		if !c.IsSummary {
			t.Error("IsSummary should be true")
		}
		if c.Text != "This is the summary." {
			t.Errorf("Text: got %q", c.Text)
		}
		if c.IsAttachment {
			t.Error("IsAttachment should be false")
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if cs := s.SummaryChunks(""); len(cs) != 0 {
			t.Errorf("chunks for empty input: got %d, want 0", len(cs))
		}
	})

	t.Run("whitespace-only", func(t *testing.T) {
		t.Parallel()
		if cs := s.SummaryChunks("   \n\t  "); len(cs) != 0 {
			t.Errorf("chunks for whitespace-only input: got %d, want 0", len(cs))
		}
	})

	t.Run("capped summary splits into pieces within the cap", func(t *testing.T) {
		t.Parallel()
		capped := NewSplitter().WithMaxRunes(10)
		cs := capped.SummaryChunks(strings.Repeat("y", 25))
		if len(cs) != 3 {
			t.Fatalf("chunks: got %d, want 3", len(cs))
		}
		for _, c := range cs {
			if n := utf8.RuneCountInString(c.Text); n > 10 {
				t.Errorf("chunk runes: got %d, want at most 10", n)
			}
		}
	})
}

// TestFirstSentence isolates the heuristic so the contract is documented.
func TestFirstSentence(t *testing.T) {
	t.Parallel()

	cases := []struct{ in, want string }{
		{"Plain summary.", "Plain summary."},
		{"First. Second.", "First."},
		{"What about questions? Yes.", "What about questions?"},
		{"Excitement! Continues.", "Excitement!"},
		{"No terminator", "No terminator"},
		{"  Leading whitespace.  ", "Leading whitespace."},
		{"", ""},
		// Known limitation of the simple terminator-then-whitespace
		// heuristic: an abbreviation whose trailing `.` is followed by a
		// space ends the "sentence" early. Acceptable trade-off — keeps
		// the function dependency-free and the misclassification only
		// shortens the prepended `Entry:` line.
		{"Mid-sentence dotted abbrev e.g. matters here.", "Mid-sentence dotted abbrev e.g."},
	}
	for _, c := range cases {
		if got := firstSentence(c.in); got != c.want {
			t.Errorf("firstSentence(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

// TestStripFrontmatter covers edge cases of the frontmatter parser.
func TestStripFrontmatter(t *testing.T) {
	t.Parallel()

	t.Run("no-frontmatter", func(t *testing.T) {
		t.Parallel()
		body, fm, err := stripFrontmatter("just text")
		if err != nil {
			t.Fatal(err)
		}
		if body != "just text" {
			t.Errorf("body: got %q", body)
		}
		if fm != nil {
			t.Errorf("fm: got %#v", fm)
		}
	})

	t.Run("simple-frontmatter", func(t *testing.T) {
		t.Parallel()
		body, fm, err := stripFrontmatter("---\nkey: value\n---\nbody")
		if err != nil {
			t.Fatal(err)
		}
		if body != "body" {
			t.Errorf("body: got %q", body)
		}
		if fm["key"] != "value" {
			t.Errorf("fm[key]: got %#v", fm["key"])
		}
	})

	t.Run("crlf-frontmatter", func(t *testing.T) {
		t.Parallel()
		body, fm, err := stripFrontmatter("---\r\nkey: value\r\n---\r\nbody")
		if err != nil {
			t.Fatal(err)
		}
		if body != "body" {
			t.Errorf("body: got %q", body)
		}
		if fm["key"] != "value" {
			t.Errorf("fm[key]: got %#v", fm["key"])
		}
	})

	t.Run("unterminated", func(t *testing.T) {
		t.Parallel()
		_, _, err := stripFrontmatter("---\nkey: value\nbody")
		if err == nil {
			t.Error("expected error for unterminated frontmatter")
		}
	})

	t.Run("hr-not-frontmatter", func(t *testing.T) {
		// A body that starts with `---` on a line of its own (e.g. a
		// thematic break) but not as a frontmatter delimiter should pass
		// through unchanged. We treat `---\n` at offset 0 as the only
		// signal — anything else is body content.
		t.Parallel()
		body, fm, err := stripFrontmatter("\n---\n\ntext")
		if err != nil {
			t.Fatal(err)
		}
		if body != "\n---\n\ntext" {
			t.Errorf("body: got %q", body)
		}
		if fm != nil {
			t.Errorf("fm: got %#v", fm)
		}
	})
}

func TestSplitter_MaxRunes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		markdown  string
		summary   string
		maxRunes  int
		wantErr   bool
		wantChunk int
	}{
		{name: "uncapped keeps one chunk", markdown: "## H\n" + strings.Repeat("x", 500), maxRunes: 0, wantChunk: 1},
		{name: "fits the cap", markdown: "## H\nshort", maxRunes: 200, wantChunk: 1},
		{name: "body over cap splits", markdown: "## H\n" + strings.Repeat("字", 300), maxRunes: 100, wantChunk: 4},
		{name: "preamble leaves no room", markdown: "## H\nbody", summary: "S.", maxRunes: 1, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := NewSplitter().WithMaxRunes(tc.maxRunes).Split(SplitInput{Markdown: tc.markdown, EntrySummary: tc.summary})
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error when the preamble leaves no room")
				}
				return
			}
			if err != nil {
				t.Fatalf("Split: %v", err)
			}
			if len(out.Chunks) != tc.wantChunk {
				t.Fatalf("chunks: got %d, want %d", len(out.Chunks), tc.wantChunk)
			}
			for _, c := range out.Chunks {
				if tc.maxRunes > 0 && utf8.RuneCountInString(c.Text) > tc.maxRunes {
					t.Errorf("chunk runes: got %d, want at most %d", utf8.RuneCountInString(c.Text), tc.maxRunes)
				}
			}
		})
	}
}
