package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Anchor proof: a piece served in full carries a
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
	// Challenges are the later proofs of the same piece, one per pointer
	// served for it: the word right after Prompt, which occurs once in the
	// piece. A proof that worked never works twice, so only the text proves
	// the piece again.
	Challenges []Challenge
}

// AnchorChallenges is how many later proofs a piece served in full carries
// before it serves in full again.
const AnchorChallenges = 3

// Challenge asks for the word right after Prompt in a piece's text.
type Challenge struct {
	Prompt string
	Answer string
}

// letterRuns counts each run of letters in text, case folded. A prompt is a
// run that occurs once in the text a piece is served within, so no other
// spot, as "user" in "user's" or a word of a piece beside it, reads as the
// one it names.
func letterRuns(text string) map[string]int {
	count := map[string]int{}
	for _, run := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) }) {
		count[strings.ToLower(run)]++
	}
	return count
}

// pickChallenges draws up to n challenges from text: content words whose
// letter run count names once, each with the content word right after it on
// the same line. It takes the ones nearest at, the tag's offset, so the agent
// finds each prompt beside the tag it holds.
func pickChallenges(text string, count map[string]int, at, n int) []Challenge {
	words := contentWords(text)
	type candidate struct {
		challenge Challenge
		distance  int
	}
	// Every proof names another word, so none answers what an earlier one did.
	used := map[string]bool{}
	if word, ok := leadingWord(text[at:]); ok {
		used[strings.ToLower(word)] = true
	}
	var candidates []candidate
	for i, w := range words {
		if count[strings.ToLower(w.text)] != 1 || w.offset == at || i+1 == len(words) {
			continue
		}
		rest := text[w.offset+len(w.text):]
		gap := rest[:len(rest)-len(strings.TrimLeftFunc(rest, unicode.IsSpace))]
		// A function word after the prompt could be guessed without the text,
		// so only a content word right after it answers.
		next := words[i+1]
		if strings.Contains(gap, "\n") || next.offset != w.offset+len(w.text)+len(gap) {
			continue
		}
		distance := w.offset - at
		if distance < 0 {
			distance = -distance
		}
		candidates = append(candidates, candidate{Challenge{Prompt: w.text, Answer: next.text}, distance})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].distance < candidates[j].distance })
	var picked []Challenge
	for _, c := range candidates {
		answer := strings.ToLower(c.challenge.Answer)
		if len(picked) == n || used[answer] {
			continue
		}
		used[answer] = true
		picked = append(picked, c.challenge)
	}
	return picked
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

// TagName is the tag as a pointer names it: without the mark, so no word
// after it reads as its proof.
func TagName(tag string) string { return "tag " + tag }

// PlaceAnchor places tag before a content word of text drawn from rnd. It
// reports false when the text holds no content word outside code.
func PlaceAnchor(text, tag string, rnd RandomSource) (Anchor, bool) {
	return PlaceAnchorIn(text, text, tag, rnd)
}

// PlaceAnchorIn is PlaceAnchor for text served within field, the whole
// result text it is joined into: each later proof's prompt occurs once in
// field.
func PlaceAnchorIn(text, field, tag string, rnd RandomSource) (Anchor, bool) {
	words := contentWords(text)
	if len(words) == 0 {
		return Anchor{}, false
	}
	count := letterRuns(field)
	i, challenges := drawKeepingProofs(len(words), rnd, func(i int) []Challenge {
		return pickChallenges(text, count, words[i].offset, AnchorChallenges)
	})
	w := words[i]
	return Anchor{Tag: tag, Word: w.text, Offset: w.offset, Challenges: challenges}, true
}

// drawKeepingProofs draws one of n tag positions from rnd. A draw that keeps
// fewer later proofs than another position would is drawn again among the
// positions keeping the most, so a short piece's pointers do not end on an
// unlucky draw. Past redrawLimit positions the first draw stands, since each
// position costs a pass over its text.
func drawKeepingProofs(n int, rnd RandomSource, challengesAt func(int) []Challenge) (int, []Challenge) {
	const redrawLimit = 400
	i := rnd.IntN(n)
	challenges := challengesAt(i)
	if len(challenges) == AnchorChallenges || n > redrawLimit {
		return i, challenges
	}
	var best []int
	most := len(challenges)
	for j := range n {
		switch got := len(challengesAt(j)); {
		case got > most:
			most, best = got, []int{j}
		case got == most && got > len(challenges):
			best = append(best, j)
		}
	}
	if len(best) == 0 {
		return i, challenges
	}
	i = best[rnd.IntN(len(best))]
	return i, challengesAt(i)
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
		text string
		word textWord
	}
	var spots []spot
	walkSchemaDescriptions(schema, nil, func(path []string, text string) {
		for _, w := range contentWords(text) {
			spots = append(spots, spot{path: path, text: text, word: w})
		}
	})
	if len(spots) == 0 {
		return Anchor{}, false
	}
	// The challenges come from the description the tag sits in, so the agent
	// finds each prompt beside the tag.
	i, challenges := drawKeepingProofs(len(spots), rnd, func(i int) []Challenge {
		return pickChallenges(spots[i].text, letterRuns(spots[i].text), spots[i].word.offset, AnchorChallenges)
	})
	s := spots[i]
	return Anchor{Tag: tag, Word: s.word.text, Offset: s.word.offset, Path: s.path, Challenges: challenges}, true
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
	inCode, afterTag := false, false
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
			token := line[i:end]
			// The word after another tag is that tag's proof.
			if !inCode && !afterTag {
				if w, ok := tokenContentWord(token); ok {
					w.offset += base + i
					words = append(words, w)
				}
			}
			afterTag = strings.Contains(token, anchorTagOpen)
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

// EntryBrief is an entry's brief: the first line of its summary, else of its
// body. Never cut.
func EntryBrief(e *Entry) string {
	brief := strings.TrimSpace(e.Summary)
	if i := strings.IndexAny(brief, "\n"); i >= 0 {
		brief = brief[:i]
	}
	if brief == "" {
		brief = strings.TrimSpace(e.Content)
		if i := strings.IndexAny(brief, "\n"); i >= 0 {
			brief = brief[:i]
		}
	}
	return brief
}

// EntryHash keys an entry's stored text, so a proof of the entry served
// before an edit in place stops holding it.
func EntryHash(e *Entry) string {
	sum := sha256.Sum256([]byte(e.Summary + "\n\n" + e.Content))
	return hex.EncodeToString(sum[:8])
}

// placedAnchorTag is an anchor tag as sdd places it before a word, with the
// space after it.
var placedAnchorTag = regexp.MustCompile(anchorTagOpen + `[0-9a-z]{` + strconv.Itoa(anchorTagLen) + `}` + anchorTagClose + ` ?`)

// StripAnchorTags takes every anchor tag out of text an agent sends back, so
// a passage it quotes from served text never carries a tag into a draft, a
// relayed word or a quote that must match the source.
func StripAnchorTags(text string) string {
	if !strings.Contains(text, anchorTagOpen) {
		return text
	}
	return placedAnchorTag.ReplaceAllString(text, "")
}

// nextProofClause matches a NextProof clause in served text.
var nextProofClause = regexp.MustCompile(`next proof [0-9a-z]{4}:<the word right after "(?:[^"\\]|\\.)*">`)

// heldTagName matches the tag an entry pointer names (TagName).
var heldTagName = regexp.MustCompile(`\(held as tag [0-9a-z]{4}`)

// WithoutProofs returns text without its anchor tags, the tags its entry
// pointers name and its next-proof clauses: the parts that change from serve
// to serve while the text is the same piece.
func WithoutProofs(text string) string {
	return nextProofClause.ReplaceAllString(heldTagName.ReplaceAllString(StripAnchorTags(text), "(held as"), "")
}

// NextProof tells how the next call proves tag: the word right after next in
// the piece's text (Challenge). Empty without a next challenge.
func NextProof(tag, next string) string {
	if next == "" {
		return ""
	}
	return fmt.Sprintf("next proof %s:<the word right after %q>", tag, next)
}
