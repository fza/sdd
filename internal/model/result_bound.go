package model

import (
	"strings"
	"unicode/utf8"
)

// MaxResultChars bounds the text content of one MCP result
// (20261112-075604-d-tac-t4r): Claude Code saves a larger one to a file the
// agent then reads back.
const MaxResultChars = 50000

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
// paragraph within room where there is one, else after the last line, else
// between runes.
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
	if i := strings.LastIndex(head, "\n\n"); i > 0 {
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
