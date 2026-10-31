package textsplitter

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Splitter is the high-level entry point for chunking SDD entry bodies and
// attachments. It strips YAML frontmatter at the package boundary, runs the
// markdown splitter, and composes the `Entry: <summary>\nBreadcrumb: <chain>`
// preamble onto each chunk's Text. Construct via NewSplitter.
type Splitter struct {
	markdown *MarkdownTextSplitter
	maxRunes int
}

// NewSplitter constructs a Splitter with the given markdown splitter
// options. Defaults match DefaultOptions() (CodeBlocks=true,
// ChunkSize=DefaultChunkSize, ChunkOverlap=DefaultChunkOverlap).
func NewSplitter(opts ...Option) *Splitter {
	return &Splitter{markdown: NewMarkdownTextSplitter(opts...)}
}

// WithMaxRunes returns a copy whose chunk Text, Entry preamble included,
// holds at most n runes. n <= 0 leaves chunks uncapped.
func (s *Splitter) WithMaxRunes(n int) *Splitter {
	capped := *s
	capped.maxRunes = n
	return &capped
}

// MaxRunes reports the cap set by WithMaxRunes, zero when uncapped.
func (s *Splitter) MaxRunes() int {
	return s.maxRunes
}

// SplitInput captures everything the splitter needs to produce per-chunk
// output for a single source document.
type SplitInput struct {
	// Markdown is the document text. May start with a YAML frontmatter block
	// delimited by `---` lines; if so it is stripped before tokenization
	// and surfaced as Frontmatter on the result.
	Markdown string
	// EntrySummary is prepended to each chunk's Text as `Entry: <first
	// sentence>` so embeddings carry entry-level identity (per d-tac-jvd).
	// Only the first sentence is used; pass the entry's stored summary
	// verbatim and the splitter handles trimming.
	EntrySummary string
	// IsAttachment marks this input as an attachment. Each chunk's
	// IsAttachment flag is set accordingly and SourceAttachmentPath is
	// recorded on each chunk.
	IsAttachment bool
	// SourceAttachmentPath is the relative path of the source attachment
	// (e.g. `2026/05/04-235258-d-tac-lqr/design.md`). Recorded on each
	// chunk when IsAttachment is true.
	SourceAttachmentPath string
}

// SplitOutput carries the per-input results: chunks ready to embed plus the
// stripped frontmatter as a generic map for the caller's filterable
// metadata layer (frontmatter values are not embedded as text per
// d-tac-lqr).
type SplitOutput struct {
	Chunks      []Chunk
	Frontmatter map[string]any
}

// Split chunks input.Markdown. Heading-only sections produce no chunk;
// entries without `##` headings produce a single body chunk after summary
// is added separately by the caller. Frontmatter is parsed from the
// document head (delimited by `---` lines) and returned via
// SplitOutput.Frontmatter.
func (s *Splitter) Split(input SplitInput) (SplitOutput, error) {
	body, fm, err := stripFrontmatter(input.Markdown)
	if err != nil {
		return SplitOutput{}, err
	}

	raws, err := s.markdown.SplitChunks(body)
	if err != nil {
		return SplitOutput{}, fmt.Errorf("markdown splitter: %w", err)
	}

	summaryFirstSentence := firstSentence(input.EntrySummary)
	if s.maxRunes > 0 {
		summaryFirstSentence = truncateRunes(summaryFirstSentence, s.maxRunes/4)
	}

	out := SplitOutput{Frontmatter: fm}
	for _, raw := range raws {
		bodies, err := s.fitBodies(summaryFirstSentence, raw.Breadcrumb, raw.Body)
		if err != nil {
			return SplitOutput{}, err
		}
		for _, body := range bodies {
			out.Chunks = append(out.Chunks, Chunk{
				Text:                 composePreamble(summaryFirstSentence, raw.Breadcrumb, body),
				Body:                 body,
				Breadcrumb:           raw.Breadcrumb,
				Depth:                raw.Depth,
				IsAttachment:         input.IsAttachment,
				SourceAttachmentPath: input.SourceAttachmentPath,
			})
		}
	}
	return out, nil
}

// fitBodies returns body unchanged when the cap is off or the chunk fits.
// Otherwise it cuts body into pieces that, with the preamble, fit the cap.
// A preamble that leaves no room for body is an error, never a silent cut.
func (s *Splitter) fitBodies(summary string, breadcrumb []string, body string) ([]string, error) {
	if s.maxRunes <= 0 || utf8.RuneCountInString(composePreamble(summary, breadcrumb, body)) <= s.maxRunes {
		return []string{body}, nil
	}
	room := s.maxRunes - utf8.RuneCountInString(composePreamble(summary, breadcrumb, ""))
	if room < 1 {
		return nil, fmt.Errorf("preamble of %d runes leaves no room in a %d-rune cap", s.maxRunes-room, s.maxRunes)
	}
	runes := []rune(body)
	var pieces []string
	for start := 0; start < len(runes); start += room {
		pieces = append(pieces, string(runes[start:min(start+room, len(runes))]))
	}
	return pieces, nil
}

// SummaryChunks produces the entry-summary chunks: the raw summary, split by
// the cap when one is set. Each chunk carries IsSummary so the indexer can
// apply the depth-aware boost at retrieval time. Returns nil when summary is
// empty so callers can skip indexing entries without summaries.
func (s *Splitter) SummaryChunks(summary string) []Chunk {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}
	pieces := []string{summary}
	if s.maxRunes > 0 {
		pieces = splitRunes(summary, s.maxRunes)
	}
	chunks := make([]Chunk, 0, len(pieces))
	for _, piece := range pieces {
		chunks = append(chunks, Chunk{Text: piece, Body: piece, IsSummary: true})
	}
	return chunks
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func splitRunes(s string, n int) []string {
	runes := []rune(s)
	var pieces []string
	for start := 0; start < len(runes); start += n {
		pieces = append(pieces, string(runes[start:min(start+n, len(runes))]))
	}
	return pieces
}

// composePreamble builds the chunk text per d-tac-jvd: an `Entry:` line, a
// `Breadcrumb:` line (when breadcrumb is non-empty), a blank line, and the
// body. Empty fields are omitted; if both summary and breadcrumb are empty
// the body is returned unchanged.
func composePreamble(summary string, breadcrumb []string, body string) string {
	var b strings.Builder
	wrote := false
	if summary != "" {
		b.WriteString("Entry: ")
		b.WriteString(summary)
		b.WriteByte('\n')
		wrote = true
	}
	if len(breadcrumb) > 0 {
		b.WriteString("Breadcrumb: ")
		b.WriteString(strings.Join(breadcrumb, " > "))
		b.WriteByte('\n')
		wrote = true
	}
	if wrote {
		b.WriteByte('\n')
	}
	b.WriteString(body)
	return b.String()
}

// firstSentence returns the leading sentence of s. It matches sentence-
// terminators at "." "!" "?" followed by whitespace or end-of-string,
// after trimming surrounding whitespace. No-period inputs return the
// trimmed string. An empty input returns empty.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' {
			// Check the next byte (if any) for whitespace or end. The
			// loop variable's i is byte-based even though r is a rune,
			// which is what we want when slicing.
			next := i + 1
			if next >= len(s) {
				return s
			}
			c := s[next]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				return s[:next]
			}
		}
	}
	return s
}

// stripFrontmatter removes a leading `---\n...\n---\n` YAML block from doc
// and returns the remainder along with the parsed frontmatter map. When no
// frontmatter is present, doc is returned verbatim and the map is nil.
// Malformed frontmatter (unterminated delimiter, parse error) returns an
// error so callers can decide whether to surface it or strip and continue.
func stripFrontmatter(doc string) (string, map[string]any, error) {
	if !strings.HasPrefix(doc, "---\n") && !strings.HasPrefix(doc, "---\r\n") {
		return doc, nil, nil
	}
	// Skip the opening delimiter line.
	rest := doc
	if strings.HasPrefix(rest, "---\r\n") {
		rest = rest[5:]
	} else {
		rest = rest[4:]
	}
	// Find the closing delimiter on its own line.
	end := findFrontmatterEnd(rest)
	if end < 0 {
		return "", nil, fmt.Errorf("frontmatter opening `---` is not closed")
	}
	yamlBlock := rest[:end]
	body := rest[end:]
	body = trimLeadingDelimiter(body)

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return "", nil, fmt.Errorf("parsing frontmatter: %w", err)
	}
	return body, fm, nil
}

// findFrontmatterEnd returns the byte index of the closing `---\n` (or
// `---\r\n`, or `---` at end-of-string) line in rest, or -1 if none exists.
// Returns the index of the leading `---` so callers can slice the YAML block
// up to but not including it.
func findFrontmatterEnd(rest string) int {
	idx := 0
	for {
		// Find the next "---" preceded by a newline (or at start).
		next := strings.Index(rest[idx:], "---")
		if next < 0 {
			return -1
		}
		pos := idx + next
		// Must be at start of a line.
		isLineStart := pos == 0 || rest[pos-1] == '\n'
		// Must be followed by newline or end-of-string.
		after := pos + 3
		isLineEnd := after == len(rest) ||
			rest[after] == '\n' ||
			(rest[after] == '\r' && after+1 < len(rest) && rest[after+1] == '\n')
		if isLineStart && isLineEnd {
			return pos
		}
		idx = pos + 3
	}
}

// trimLeadingDelimiter removes the closing `---\n` (or `---\r\n`, or trailing
// `---`) from the beginning of body and returns the rest.
func trimLeadingDelimiter(body string) string {
	switch {
	case strings.HasPrefix(body, "---\r\n"):
		return body[5:]
	case strings.HasPrefix(body, "---\n"):
		return body[4:]
	case body == "---":
		return ""
	}
	return body
}
