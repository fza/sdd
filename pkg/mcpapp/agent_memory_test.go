package mcpapp_test

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	test "github.com/networkteam/sdd/internal/testsupport"
)

// provingTools are the tools that take held and reread.
var provingTools = map[string]bool{
	"start_procedure": true, "next": true, "abandon": true, "park": true,
	"search": true, "view": true, "show": true, "gather": true,
}

// agentMemory is one agent context that keeps every piece served to it: each
// call it makes lists every tag it received, with the word after it, as held.
// A fresh connection is a fresh context. It
// hands the test each result with the placed tags taken out, so assertions
// read the text as written; raw keeps them.
type agentMemory struct {
	mu   sync.Mutex
	held map[string]string
	// texts holds, per tag, the result text it came in, where the agent looks
	// up a next proof.
	texts map[string]string
	// forget drops every proof before the next call, as a lossy compaction
	// would.
	forget bool
	raw    bool
}

// guideExampleProof is the worked proof the proof guide shows; its tag was
// never issued, so an agent proving it would only draw a proof notice.
const guideExampleProof = "gn9p:first"

func newAgentMemory() *agentMemory {
	return &agentMemory{held: map[string]string{}, texts: map[string]string{}}
}

func untagged(res *mcp.CallToolResult) error {
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			text.Text = test.Untagged(text.Text)
		}
	}
	if res.StructuredContent == nil {
		return nil
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return err
	}
	var structured any
	err = json.Unmarshal([]byte(test.Untagged(string(raw))), &structured)
	if err != nil {
		return err
	}
	res.StructuredContent = structured
	return nil
}

func (m *agentMemory) proofs() []any {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.forget {
		m.held, m.forget = map[string]string{}, false
	}
	held := make([]any, 0, len(m.held))
	for tag, word := range m.held {
		held = append(held, model.Proof{Tag: tag, Word: word}.String())
	}
	return held
}

func (m *agentMemory) keep(res *mcp.CallToolResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, content := range res.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		fields := resultStrings(text.Text)
		for _, proof := range model.ReadAnchors(text.Text) {
			if proof.String() == guideExampleProof {
				continue
			}
			m.held[proof.Tag] = proof.Word
			m.texts[proof.Tag] = text.Text
			for _, field := range fields {
				if strings.Contains(field, model.AnchorMark(proof.Tag)) {
					m.texts[proof.Tag] = field
				}
			}
		}
		for _, next := range nextProof.FindAllStringSubmatch(text.Text, -1) {
			if answer, ok := wordAfter(m.texts[next[1]], next[1], next[2]); ok {
				m.held[next[1]] = answer
			}
		}
	}
}

// nextProof is a pointer's next proof as served text carries it, raw or
// JSON-escaped.
var nextProof = regexp.MustCompile(`next proof ([0-9a-z]{4}):(?:<|\\u003c)the word right after \\?"(.+?)\\?"(?:>|\\u003e)`)

// wordAfter reads the word right after prompt in the piece tag marks, as the
// agent looks it up in the text it holds. A result carries several pieces, and
// the prompt is unique only within its own piece, so an occurrence counts only
// as the server picks one: a whole word, the next word on the same line after
// whitespace alone, and no other piece's tag between it and this tag. Of those,
// the one nearest the tag is the piece's own.
func wordAfter(text, tag, prompt string) (string, bool) {
	mark := strings.Index(text, model.AnchorMark(tag))
	if mark < 0 {
		return "", false
	}
	others := otherTagSpans(text, tag)
	start, end := sectionAround(text, mark)
	best, bestOutside, answer := -1, false, ""
	for from := 0; ; {
		i := strings.Index(text[from:], prompt)
		if i < 0 {
			break
		}
		at := from + i
		from = at + len(prompt)
		if before, _ := utf8.DecodeLastRuneInString(text[:at]); at > 0 && (unicode.IsLetter(before) || unicode.IsDigit(before)) {
			continue
		}
		word, ok := nextWordOnLine(text[at+len(prompt):])
		if !ok || separated(others, at, mark) {
			continue
		}
		outside := at < start || at >= end
		if best < 0 || (bestOutside && !outside) || (outside == bestOutside && abs(at-mark) < abs(best-mark)) {
			best, bestOutside, answer = at, outside, word
		}
	}
	return answer, best >= 0
}

// sectionAround returns the bounds of the "## " section holding offset at:
// the prompt is unique within its piece, so an occurrence in the tag's own
// section is the one.
func sectionAround(text string, at int) (start, end int) {
	if i := strings.LastIndex(text[:at], "\n## "); i >= 0 {
		start = i + 1
	}
	end = len(text)
	if i := strings.Index(text[at:], "\n## "); i >= 0 {
		end = at + i
	}
	return start, end
}

// nextWordOnLine returns the word that follows rest's leading spaces on the
// same line; a tag standing between is skipped, as the agent reads past it.
func nextWordOnLine(rest string) (string, bool) {
	if r, _ := utf8.DecodeRuneInString(rest); unicode.IsLetter(r) || unicode.IsDigit(r) {
		return "", false
	}
	rest = test.Untagged(rest)
	trimmed := strings.TrimLeft(rest, " \t")
	if len(trimmed) == len(rest) {
		return "", false
	}
	end := strings.IndexFunc(trimmed, func(r rune) bool { return !unicode.IsLetter(r) })
	if end < 0 {
		end = len(trimmed)
	}
	return trimmed[:end], end > 0
}

// otherTagSpans lists the offsets of every tag in text but tag.
func otherTagSpans(text, tag string) []int {
	var at []int
	for _, proof := range model.ReadAnchors(text) {
		if proof.Tag == tag {
			continue
		}
		if i := strings.Index(text, model.AnchorMark(proof.Tag)); i >= 0 {
			at = append(at, i)
		}
	}
	return at
}

// separated reports whether another piece's tag stands between a and b.
func separated(others []int, a, b int) bool {
	lo, hi := min(a, b), max(a, b)
	for _, o := range others {
		if o > lo && o < hi {
			return true
		}
	}
	return false
}

// resultStrings decodes a JSON result into its string values, the text the
// agent reads; a text that is no JSON is its own one string.
func resultStrings(text string) []string {
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return []string{text}
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			for _, item := range v {
				walk(item)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(decoded)
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m *agentMemory) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		params, ok := req.GetParams().(*mcp.CallToolParams)
		if ok && provingTools[params.Name] {
			if args, ok := params.Arguments.(map[string]any); ok {
				if _, own := args["held"]; !own {
					withHeld := make(map[string]any, len(args)+1)
					for k, v := range args {
						withHeld[k] = v
					}
					withHeld["held"] = m.proofs()
					params.Arguments = withHeld
				}
			}
		}
		res, err := next(ctx, method, req)
		result, ok := res.(*mcp.CallToolResult)
		if !ok || err != nil {
			return res, err
		}
		m.keep(result)
		if m.raw {
			return res, nil
		}
		return result, untagged(result)
	}
}
