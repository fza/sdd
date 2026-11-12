package model

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Anchor proof (20261112-075604-d-tac-t4r): a piece served in full carries a
// tag placed before a content word; a later call proves it still holds the
// piece by naming the tag and that word.

const (
	anchorTagOpen   = "⟦"
	anchorTagClose  = "⟧"
	anchorTagLen    = 4
	anchorAlphabet  = "0123456789abcdefghijklmnopqrstuvwxyz"
	anchorMinLetter = 3
)

// RandomSource draws anchor tags and positions; math/rand/v2's *rand.Rand
// satisfies it.
type RandomSource interface {
	IntN(n int) int
}

// Anchor is a tag placed in a piece's text and the word right after it.
// Offset is the byte position in the untagged text the tag goes before; Path
// names the schema string it sits in when the piece is a JSON schema.
type Anchor struct {
	Tag    string
	Word   string
	Offset int
	Path   []string
}

// Proof is a call's claim that it holds a piece: a tag and the word after it.
type Proof struct {
	Tag  string
	Word string
}

func (p Proof) String() string { return p.Tag + ":" + p.Word }

// NewAnchorTag draws a tag of four lowercase base36 characters.
func NewAnchorTag(rnd RandomSource) string {
	var b strings.Builder
	for range anchorTagLen {
		b.WriteByte(anchorAlphabet[rnd.IntN(len(anchorAlphabet))])
	}
	return b.String()
}

// AnchorMark is the tag as it appears in served text.
func AnchorMark(tag string) string { return anchorTagOpen + tag + anchorTagClose }

// PlaceAnchor places tag before a content word of text drawn from rnd. It
// reports false when the text holds no content word outside code.
func PlaceAnchor(text, tag string, rnd RandomSource) (Anchor, bool) {
	words := contentWords(text)
	if len(words) == 0 {
		return Anchor{}, false
	}
	w := words[rnd.IntN(len(words))]
	return Anchor{Tag: tag, Word: w.text, Offset: w.offset}, true
}

// Render returns text with the anchor's tag placed.
func (a Anchor) Render(text string) string {
	if a.Tag == "" || a.Offset < 0 || a.Offset > len(text) {
		return text
	}
	return text[:a.Offset] + AnchorMark(a.Tag) + " " + text[a.Offset:]
}

// PlaceSchemaAnchor places tag before a content word of one of the schema's
// description strings, drawn from rnd. It reports false when no description
// holds a content word.
func PlaceSchemaAnchor(schema map[string]any, tag string, rnd RandomSource) (Anchor, bool) {
	type spot struct {
		path []string
		word textWord
	}
	var spots []spot
	walkSchemaDescriptions(schema, nil, func(path []string, text string) {
		for _, w := range contentWords(text) {
			spots = append(spots, spot{path: path, word: w})
		}
	})
	if len(spots) == 0 {
		return Anchor{}, false
	}
	s := spots[rnd.IntN(len(spots))]
	return Anchor{Tag: tag, Word: s.word.text, Offset: s.word.offset, Path: s.path}, true
}

// RenderSchema returns a copy of schema with the anchor's tag placed in the
// string its path names; maps on the path are copied, the rest is shared.
func (a Anchor) RenderSchema(schema map[string]any) map[string]any {
	if a.Tag == "" || len(a.Path) == 0 {
		return schema
	}
	return renderSchemaAt(schema, a.Path, a)
}

func renderSchemaAt(node map[string]any, path []string, a Anchor) map[string]any {
	out := make(map[string]any, len(node))
	for k, v := range node {
		out[k] = v
	}
	switch child := node[path[0]].(type) {
	case string:
		if len(path) == 1 {
			out[path[0]] = a.Render(child)
		}
	case map[string]any:
		if len(path) > 1 {
			out[path[0]] = renderSchemaAt(child, path[1:], a)
		}
	}
	return out
}

func walkSchemaDescriptions(node map[string]any, path []string, visit func([]string, string)) {
	keys := make([]string, 0, len(node))
	for k := range node {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		at := append(append([]string(nil), path...), k)
		switch v := node[k].(type) {
		case string:
			if k == "description" {
				visit(at, v)
			}
		case map[string]any:
			walkSchemaDescriptions(v, at, visit)
		}
	}
}

// ParseProof reads a "tag:word" proof. The tag may carry its brackets.
func ParseProof(s string) (Proof, error) {
	tag, word, ok := strings.Cut(strings.TrimSpace(s), ":")
	tag = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(tag), anchorTagOpen), anchorTagClose)))
	word = strings.TrimSpace(word)
	if !ok || !validAnchorTag(tag) || word == "" {
		return Proof{}, fmt.Errorf("%q is not a proof: write the tag and the word right after it as tag:word", s)
	}
	return Proof{Tag: tag, Word: word}, nil
}

// ProofWordMatches reports whether a proof's word names the issued word:
// case-insensitive, surrounding punctuation trimmed.
func ProofWordMatches(issued, given string) bool {
	trim := func(s string) string {
		return strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	given = trim(given)
	return given != "" && strings.EqualFold(trim(issued), given)
}

// WordAfterAnchor reads the word right after tag in text.
func WordAfterAnchor(text, tag string) (string, bool) {
	mark := AnchorMark(tag)
	i := strings.Index(text, mark)
	if i < 0 {
		return "", false
	}
	return leadingWord(text[i+len(mark):])
}

// ReadAnchors reads every tag in text with the word right after it, in order.
func ReadAnchors(text string) []Proof {
	var proofs []Proof
	for {
		i := strings.Index(text, anchorTagOpen)
		if i < 0 {
			return proofs
		}
		text = text[i+len(anchorTagOpen):]
		end := strings.Index(text, anchorTagClose)
		if end < 0 {
			return proofs
		}
		tag := text[:end]
		text = text[end+len(anchorTagClose):]
		if !validAnchorTag(tag) {
			continue
		}
		if word, ok := leadingWord(text); ok {
			proofs = append(proofs, Proof{Tag: tag, Word: word})
		}
	}
}

func leadingWord(s string) (string, bool) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	end := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsLetter(r) })
	if end < 0 {
		end = len(s)
	}
	return s[:end], end > 0
}

func validAnchorTag(tag string) bool {
	if len(tag) != anchorTagLen {
		return false
	}
	for _, r := range tag {
		if !strings.ContainsRune(anchorAlphabet, r) {
			return false
		}
	}
	return true
}

type textWord struct {
	text   string
	offset int
}

// contentWords lists the words a tag may stand before: letters only, at
// least three of them, not a function word, outside code spans and fences,
// and not a key ending in a colon.
func contentWords(text string) []textWord {
	var words []textWord
	inFence := false
	offset := 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		} else if !inFence {
			words = append(words, lineContentWords(line, offset)...)
		}
		offset += len(line)
	}
	return words
}

func lineContentWords(line string, base int) []textWord {
	var words []textWord
	inCode := false
	i := 0
	for i < len(line) {
		r, size := utf8.DecodeRuneInString(line[i:])
		switch {
		case r == '`':
			inCode = !inCode
			i += size
		case unicode.IsSpace(r):
			i += size
		default:
			end := i
			for end < len(line) {
				r, size := utf8.DecodeRuneInString(line[end:])
				if unicode.IsSpace(r) || r == '`' {
					break
				}
				end += size
			}
			if !inCode {
				if w, ok := tokenContentWord(line[i:end]); ok {
					w.offset += base + i
					words = append(words, w)
				}
			}
			i = end
		}
	}
	return words
}

func tokenContentWord(token string) (textWord, bool) {
	if strings.Contains(token, anchorTagOpen) || strings.HasSuffix(token, ":") {
		return textWord{}, false
	}
	start := strings.IndexFunc(token, func(r rune) bool { return !unicode.IsPunct(r) && !unicode.IsSymbol(r) })
	if start < 0 {
		return textWord{}, false
	}
	word := strings.TrimRightFunc(token[start:], func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) })
	if utf8.RuneCountInString(word) < anchorMinLetter || functionWords[strings.ToLower(word)] {
		return textWord{}, false
	}
	for _, r := range word {
		if !unicode.IsLetter(r) {
			return textWord{}, false
		}
	}
	return textWord{text: word, offset: start}, true
}

var functionWords = func() map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.Fields(`about above after again against all also and any are because been before being
		below between both but can could did does doing down during each else ever every few for from
		further had has have having her here hers him his how into its itself just let may might more most
		must nor not now off once only other our ours out over own same she should some such than that
		the their theirs them then there these they this those through too under until upon very was were
		what when where which while who whom whose why will with within without would yet you your yours
		one two per via its it's isn't don't doesn't won't can't`) {
		words[w] = true
	}
	return words
}()
