package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RandomSource draws question kinds and phrases; math/rand/v2's *rand.Rand
// satisfies it.
type RandomSource interface {
	IntN(n int) int
}

// QuestionKind is one of the seven kinds of text question
// (20261121-230157-d-tac-ym8). None counts paragraphs or words.
type QuestionKind string

const (
	AskSentenceFirst QuestionKind = "sentence-first"
	AskSentenceLast  QuestionKind = "sentence-last"
	AskBetween       QuestionKind = "between"
	AskNextSentence  QuestionKind = "next-sentence"
	AskNumberAfter   QuestionKind = "number-after"
	AskWordsAfter    QuestionKind = "words-after"
	AskWordsBefore   QuestionKind = "words-before"
)

// QuestionKinds lists the kinds a question is drawn from.
var QuestionKinds = []QuestionKind{
	AskSentenceFirst, AskSentenceLast, AskBetween, AskNextSentence, AskNumberAfter, AskWordsAfter, AskWordsBefore,
}

// LostAnswer is the answer of an agent that no longer holds the text asked
// about.
const LostAnswer = "lost"

// phraseWords is how many words a phrase holds.
const phraseWords = 3

// TextQuestion asks for words of a served text next to a phrase, a run of
// words that occurs once in it. Second is the phrase after the word asked
// for, for AskBetween alone.
type TextQuestion struct {
	Kind   QuestionKind
	Phrase string
	Second string
	Answer string
}

// Key identifies the question within the text it asks about, so it is never
// asked twice.
func (q TextQuestion) Key() string {
	return string(q.Kind) + "|" + q.Phrase + "|" + q.Second
}

// Ask is the question without the text it asks about.
func (q TextQuestion) Ask() string {
	switch q.Kind {
	case AskSentenceFirst:
		return fmt.Sprintf("first 2 words of the sentence with %q?", q.Phrase)
	case AskSentenceLast:
		return fmt.Sprintf("last 2 words of the sentence with %q?", q.Phrase)
	case AskBetween:
		return fmt.Sprintf("word between %q and %q?", q.Phrase, q.Second)
	case AskNextSentence:
		return fmt.Sprintf("first 2 words of the sentence after the one with %q?", q.Phrase)
	case AskNumberAfter:
		return fmt.Sprintf("number after %q?", q.Phrase)
	case AskWordsBefore:
		return fmt.Sprintf("2 words before %q?", q.Phrase)
	default:
		return fmt.Sprintf("2 words after %q?", q.Phrase)
	}
}

// Line is the question as a response ends with it, about the text named
// name.
func (q TextQuestion) Line(name string) string {
	return "q " + name + ": " + q.Ask() + " | " + LostAnswer
}

// IsLost reports whether given is the answer lost.
func IsLost(given string) bool {
	return strings.EqualFold(strings.Trim(strings.TrimSpace(given), `"'.`), LostAnswer)
}

// Answered reports whether given names the words the question asks for:
// case, quotes and surrounding punctuation aside, and digit grouping aside
// for a number.
func (q TextQuestion) Answered(given string) bool {
	want := answerWords(q.Answer)
	return len(want) > 0 && slices.Equal(want, answerWords(given))
}

func answerWords(s string) []string {
	var words []string
	for _, field := range strings.Fields(s) {
		word := strings.ToLower(wordForm(field))
		if word == "" {
			continue
		}
		if numberWord(word) {
			word = strings.ReplaceAll(word, ",", "")
		}
		words = append(words, word)
	}
	return words
}

// DrawQuestion makes a question about text: a kind drawn from rnd, then a
// phrase of that kind, trying the next kind when text holds none. usable
// rejects a question already asked or whose phrase occurs in another text
// the agent holds. It reports false when text holds no usable question.
func DrawQuestion(text string, rnd RandomSource, usable func(TextQuestion) bool) (TextQuestion, bool) {
	parsed := parseQuestionText(text)
	kinds := append([]QuestionKind(nil), QuestionKinds...)
	shuffle(rnd, len(kinds), func(i, j int) { kinds[i], kinds[j] = kinds[j], kinds[i] })
	for _, kind := range kinds {
		candidates := parsed.candidates(kind)
		shuffle(rnd, len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
		for _, q := range candidates {
			if usable == nil || usable(q) {
				return q, true
			}
		}
	}
	return TextQuestion{}, false
}

func shuffle(rnd RandomSource, n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		swap(i, rnd.IntN(i+1))
	}
}

// textToken is a whitespace-separated token of a text: raw as written, word
// its letters and digits without surrounding punctuation, start and end its
// byte offsets.
type textToken struct {
	raw, word  string
	start, end int
}

// textSentence is a run of tokens up to a sentence end, a blank line, a list
// item or a heading; para numbers the paragraph it sits in.
type textSentence struct {
	tokens []textToken
	para   int
}

type questionText struct {
	text      string
	lower     string
	sentences []textSentence
}

func parseQuestionText(text string) questionText {
	parsed := questionText{text: text, lower: strings.ToLower(text)}
	var current textSentence
	para := 0
	closeSentence := func() {
		if len(current.tokens) > 0 {
			parsed.sentences = append(parsed.sentences, current)
		}
		current = textSentence{para: para}
	}
	inFence := false
	offset := 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		base := offset
		offset += len(line)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			closeSentence()
			para++
			current.para = para
			continue
		}
		if inFence {
			continue
		}
		if trimmed == "" {
			closeSentence()
			para++
			current.para = para
			continue
		}
		first, _ := utf8.DecodeRuneInString(trimmed)
		heading := strings.HasPrefix(trimmed, "#")
		if !unicode.IsLower(first) || heading {
			closeSentence()
		}
		if heading {
			para++
			current.para = para
		}
		atLineStart := true
		for start, raw := range lineTokens(line) {
			if atLineStart && listMarker(raw) {
				atLineStart = false
				continue
			}
			atLineStart = false
			word := wordForm(raw)
			if word == "" {
				continue
			}
			current.tokens = append(current.tokens, textToken{raw: raw, word: word, start: base + start, end: base + start + len(raw)})
			if endsSentence(raw, word) {
				closeSentence()
			}
		}
		if heading {
			closeSentence()
			para++
			current.para = para
		}
	}
	closeSentence()
	return parsed
}

// lineTokens yields each whitespace-separated token of line with its byte
// offset.
func lineTokens(line string) func(yield func(int, string) bool) {
	return func(yield func(int, string) bool) {
		start := -1
		for i, r := range line {
			if unicode.IsSpace(r) {
				if start >= 0 && !yield(start, line[start:i]) {
					return
				}
				start = -1
				continue
			}
			if start < 0 {
				start = i
			}
		}
		if start >= 0 {
			yield(start, line[start:])
		}
	}
}

func listMarker(raw string) bool {
	switch raw {
	case "-", "*", "+", ">":
		return true
	}
	digits := strings.TrimRight(raw, ".)")
	return digits != raw && digits != "" && strings.IndexFunc(digits, func(r rune) bool { return !unicode.IsDigit(r) }) < 0
}

func wordForm(raw string) string {
	return strings.TrimFunc(raw, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// endsSentence reports whether a token closes its sentence: it ends in a
// full stop, question or exclamation mark, quotes and brackets aside, and is
// no abbreviation such as "e.g.".
func endsSentence(raw, word string) bool {
	tail := strings.TrimRight(raw, ")]}\"'*_`")
	if tail == "" || !strings.ContainsAny(tail[len(tail)-1:], ".!?") {
		return false
	}
	return !strings.Contains(word, ".")
}

// plainToken reports whether a token is a word as written, so a phrase of
// such tokens occurs literally in the text.
func plainToken(t textToken) bool {
	return t.raw == t.word && answerable(t.word)
}

// answerable reports whether an agent types word back reliably: letters and
// digits, a hyphen or apostrophe inside.
func answerable(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '\'' && r != '’' {
			return false
		}
	}
	return true
}

func numberWord(word string) bool {
	digits := 0
	for _, r := range word {
		switch {
		case unicode.IsDigit(r):
			digits++
		case r == ',' || r == '.':
		default:
			return false
		}
	}
	return digits > 0
}

// phraseAt returns the phrase of the tokens of s from i on, when they occur
// literally and once in the text and not all are function words.
func (p questionText) phraseAt(s textSentence, i int) (string, bool) {
	if i < 0 || i+phraseWords > len(s.tokens) {
		return "", false
	}
	return p.literal(s.tokens[i:i+phraseWords], func(phrase string) bool {
		content := false
		for _, t := range s.tokens[i : i+phraseWords] {
			content = content || !functionWords[strings.ToLower(t.word)]
		}
		return content && strings.Count(p.lower, strings.ToLower(phrase)) == 1
	})
}

// literal joins tokens with single spaces when that is how the text holds
// them, each a plain token, and keep accepts the result.
func (p questionText) literal(tokens []textToken, keep func(string) bool) (string, bool) {
	raws := make([]string, len(tokens))
	for i, t := range tokens {
		if !plainToken(t) {
			return "", false
		}
		raws[i] = t.raw
	}
	joined := strings.Join(raws, " ")
	if p.text[tokens[0].start:tokens[len(tokens)-1].end] != joined || !keep(joined) {
		return "", false
	}
	return joined, true
}

func answerOf(tokens ...textToken) (string, bool) {
	words := make([]string, len(tokens))
	for i, t := range tokens {
		if !answerable(t.word) {
			return "", false
		}
		words[i] = t.word
	}
	return strings.Join(words, " "), true
}

// candidates lists every question of kind the text holds.
func (p questionText) candidates(kind QuestionKind) []TextQuestion {
	var out []TextQuestion
	for si, s := range p.sentences {
		n := len(s.tokens)
		for i := range s.tokens {
			phrase, ok := p.phraseAt(s, i)
			if !ok {
				continue
			}
			end := i + phraseWords
			var answer string
			q := TextQuestion{Kind: kind, Phrase: phrase}
			switch kind {
			case AskWordsAfter:
				if end+2 > n {
					continue
				}
				answer, ok = answerOf(s.tokens[end], s.tokens[end+1])
			case AskWordsBefore:
				if i < 2 {
					continue
				}
				answer, ok = answerOf(s.tokens[i-2], s.tokens[i-1])
			case AskSentenceFirst:
				if i < 2 {
					continue
				}
				answer, ok = answerOf(s.tokens[0], s.tokens[1])
			case AskSentenceLast:
				if end > n-2 {
					continue
				}
				answer, ok = answerOf(s.tokens[n-2], s.tokens[n-1])
			case AskNextSentence:
				if si+1 >= len(p.sentences) || p.sentences[si+1].para != s.para || len(p.sentences[si+1].tokens) < 2 {
					continue
				}
				next := p.sentences[si+1].tokens
				answer, ok = answerOf(next[0], next[1])
			case AskNumberAfter:
				if end >= n || !numberWord(s.tokens[end].word) {
					continue
				}
				answer = s.tokens[end].word
			case AskBetween:
				second, found := p.phraseAt(s, end+1)
				if !found {
					continue
				}
				if _, found = p.literal(s.tokens[i:end+1+phraseWords], func(string) bool { return true }); !found {
					continue
				}
				q.Second = second
				answer, ok = answerOf(s.tokens[end])
			}
			if !ok {
				continue
			}
			q.Answer = answer
			out = append(out, q)
		}
	}
	return out
}

// sessionPart stands in for a session handle in a held text. Its inner sign
// makes it no answerable word, so no question's phrase or answer holds it.
const sessionPart = "session§handle"

// SessionNeutral is text with the session handle replaced by a stand-in, so
// copies that differ only by their serving session's handle compare as one
// text (20261122-105026-d-tac-wl5).
func SessionNeutral(text, session string) string {
	if session == "" {
		return text
	}
	return strings.ReplaceAll(text, session, sessionPart)
}

// SchemaDescriptions joins the description strings of a JSON schema, in the
// key order its JSON carries them, as the text a question about the schema
// is made from: each its own paragraph.
func SchemaDescriptions(schema map[string]any) string {
	var out []string
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		for _, key := range slices.Sorted(maps.Keys(node)) {
			switch v := node[key].(type) {
			case string:
				if key == "description" {
					out = append(out, v)
				}
			case map[string]any:
				walk(v)
			}
		}
	}
	walk(schema)
	return strings.Join(out, "\n\n")
}

// EntryHash keys an entry's stored text, so an entry edited in place since
// it was served counts as a new text.
func EntryHash(e *Entry) string {
	sum := sha256.Sum256([]byte(e.Summary + "\n\n" + e.Content))
	return hex.EncodeToString(sum[:8])
}

var functionWords = func() map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.Fields(`a an about above after again against all also and any are as at be because been before being
		below between both but by can could did do does doing down during each else ever every few for from
		further had has have having he her here hers him his how i if in into is it its itself just let may me might more most
		must my no nor not now of off on once only or other our ours out over own same she should so some such than that
		the their theirs them then there these they this those through to too under until up upon very was we were
		what when where which while who whom whose why will with within without would yet you your yours
		one two per via it's isn't don't doesn't won't can't`) {
		words[w] = true
	}
	return words
}()
