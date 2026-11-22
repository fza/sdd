package model

import (
	"strings"
	"unicode/utf8"
)

// ResultBound is how many characters the text content of one MCP result may
// take, as ServedChars counts them, for the host the result is served to.
// Every other bound on a result derives from it.
type ResultBound int

const (
	// DefaultResultBound holds for every host but Claude Code: Codex prints
	// a result's JSON within a 10,000-token output budget and drops the middle
	// of a larger one, past about 19,000 characters of text.
	DefaultResultBound ResultBound = 18000
	// ClaudeCodeResultBound stays below the 50,000 characters past which
	// Claude Code saves a tool's result to a file (20261122-103723-d-tac-npg).
	ClaudeCodeResultBound ResultBound = 45000
	// ClaudeCodeClient is the MCP client name Claude Code reports.
	ClaudeCodeClient = "claude-code"
	// charsPerOutputToken turns Claude Code's MCP token limit into
	// characters, a quarter below the 2.67 measured on sdd results.
	charsPerOutputToken = 2
)

// ResultBoundFor is the bound of a result served to the MCP client named
// clientName. Only the exact name claude-code gets ClaudeCodeResultBound,
// lowered to two characters per token of mcpOutputTokens, Claude Code's
// MAX_MCP_OUTPUT_TOKENS as sdd's environment holds it (0 when unset), but
// never below DefaultResultBound, the least bound every page is sized for.
func ResultBoundFor(clientName string, mcpOutputTokens int) ResultBound {
	if clientName != ClaudeCodeClient {
		return DefaultResultBound
	}
	bound := ClaudeCodeResultBound
	if mcpOutputTokens > 0 {
		bound = min(bound, ResultBound(mcpOutputTokens*charsPerOutputToken))
	}
	return max(bound, DefaultResultBound)
}

// Serve is what one serve may take of a result, leaving room for the notices
// that join it.
func (b ResultBound) Serve() int { return int(b) - 2000 }

// Read is what the one large text field of a read may take.
func (b ResultBound) Read() int { return int(b) - 3000 }

// DeepPage bounds one page of the deep verifier's reading, leaving the rest
// of the result to the serve around the page: the verifier's rules lane, the
// report schema and the notices.
func (b ResultBound) DeepPage() int { return int(b) - 6000 }

// DeepFirstPage bounds the reading's first page tighter: the resume_session
// that serves it first carries, within Serve, the session head, the rules
// lane and the report schema in full, about 7000 characters.
func (b ResultBound) DeepFirstPage() int { return b.Serve() - 7500 }

// ServedChars is what text costs in a result as Claude Code counts it: the
// characters of the text content, which is the structured copy's JSON, so the
// text JSON-escaped once. Bytes stand in for characters, which never
// undercounts.
func ServedChars(text string) int {
	n := 0
	for _, r := range text {
		n += servedRuneChars(r)
	}
	return n
}

// ServedPrefix returns the length in bytes of text's longest prefix that
// costs at most room in a serialized result, ending between runes.
func ServedPrefix(text string, room int) int {
	spent := 0
	for i, r := range text {
		spent += servedRuneChars(r)
		if spent > room {
			return i
		}
	}
	return len(text)
}

// servedRuneChars is one rune's cost under encoding/json's escaping, HTML
// characters included.
func servedRuneChars(r rune) int {
	switch {
	case r == '"' || r == '\\' || r == '\n' || r == '\r' || r == '\t':
		return 2
	case r < 0x20 || r == '<' || r == '>' || r == '&' || r == ' ' || r == ' ' || r == utf8.RuneError:
		return 6
	case r < utf8.RuneSelf:
		return 1
	}
	return utf8.RuneLen(r)
}

// SplitServed splits text into chunks that each cost at most room in a
// serialized result, joined again the text: each breaks after the last
// paragraph within room where that keeps at least half of what fits, else
// after the last line, else between runes, so a heading over a long list of
// single lines never makes a chunk of its own.
func SplitServed(text string, room int) []string {
	var chunks []string
	for {
		fits := ServedPrefix(text, room)
		if fits == len(text) {
			return append(chunks, text)
		}
		cut := servedBreak(text, max(fits, firstRuneLen(text)))
		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}
}

func servedBreak(text string, fits int) int {
	head := text[:fits]
	if i := strings.LastIndex(head, "\n\n"); i > 0 && i+2 >= fits/2 {
		return i + 2
	}
	if i := strings.LastIndexByte(head, '\n'); i > 0 {
		return i + 1
	}
	return fits
}

func firstRuneLen(text string) int {
	_, n := utf8.DecodeRuneInString(text)
	return n
}
