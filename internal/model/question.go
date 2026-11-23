package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// RandomSource draws question kinds and phrases; math/rand/v2's *rand.Rand
// satisfies it.
type RandomSource interface {
	IntN(n int) int
}

// QuestionKind is a kind of text question (20261121-230157-d-tac-ym8). None
// counts paragraphs or words, and none rests on where a sentence ends: each
// asks for words standing next to a phrase, a run an agent and the parser
// read alike.
type QuestionKind string

const (
	AskBetween     QuestionKind = "between"
	AskNumberAfter QuestionKind = "number-after"
	AskWordsAfter  QuestionKind = "words-after"
	AskWordsBefore QuestionKind = "words-before"
)

// QuestionKinds lists the kinds a question is drawn from.
var QuestionKinds = []QuestionKind{AskBetween, AskNumberAfter, AskWordsAfter, AskWordsBefore}

// LostAnswer is the answer of an agent that no longer holds the text asked
// about.
const LostAnswer = "lost"

// AnswerSeparator parts the answers of a response's questions within one
// answer. No answer holds it: answers are words and numbers.
const AnswerSeparator = ";"

// phraseWords is how many words a phrase holds.
const phraseWords = 3

// maxAskChars bounds a question, which every response carries.
const maxAskChars = 90

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
	case AskBetween:
		return fmt.Sprintf("word between %q and %q?", q.Phrase, q.Second)
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

// RoundCredit returns the indexes of the questions of a response's round
// that given answers right, one answer each, parted by AnswerSeparator, in
// either order, each part answering one question at most. lost for any
// question, or a part count other than the round's, credits none: lost is
// how an agent reports a compaction, and a summary can carry the answer to
// one question of the round (20261122-234358-d-tac-lv2). A wrong part beside
// a right one is a misread, so the right one counts.
func RoundCredit(questions []TextQuestion, given string) []int {
	parts := strings.Split(given, AnswerSeparator)
	if len(questions) == 0 || len(parts) != len(questions) || slices.ContainsFunc(parts, IsLost) {
		return nil
	}
	used := make([]bool, len(parts))
	var best, current []int
	var match func(i int)
	match = func(i int) {
		if i == len(questions) {
			if len(current) > len(best) {
				best = slices.Clone(current)
			}
			return
		}
		for j, part := range parts {
			if used[j] || !questions[i].Answered(part) {
				continue
			}
			used[j] = true
			current = append(current, i)
			match(i + 1)
			current = current[:len(current)-1]
			used[j] = false
		}
		match(i + 1)
	}
	match(0)
	return best
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
// byte offsets. code marks a token inside an inline code span.
type textToken struct {
	raw, word  string
	start, end int
	code       bool
}

type questionText struct {
	text  string
	lower string
	// lines are the tokens of each line outside fenced code, a list marker
	// left out: a question's words stand side by side on one line.
	lines [][]textToken
}

func parseQuestionText(text string) questionText {
	parsed := questionText{text: text, lower: strings.ToLower(text)}
	inFence := false
	offset := 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		base := offset
		offset += len(line)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || trimmed == "" {
			continue
		}
		var tokens []textToken
		inCode := false
		for start, raw := range lineTokens(line) {
			if len(tokens) == 0 && listMarker(raw) {
				continue
			}
			ticks := strings.Count(raw, "`")
			tokens = append(tokens, textToken{raw: raw, word: wordForm(raw), start: base + start, end: base + start + len(raw), code: inCode || ticks > 0})
			if ticks%2 == 1 {
				inCode = !inCode
			}
		}
		parsed.lines = append(parsed.lines, tokens)
	}
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

// entryIDWord matches a full or short entry ID, which summaries carry and
// agents copy unreliably.
var entryIDWord = regexp.MustCompile(`^(\d{8}-\d{6}-)?[a-z]-[a-z]{3}-[a-z0-9]+$`)

// plainToken reports whether a token is a word as written, so a phrase of
// such tokens occurs literally in the text.
func plainToken(t textToken) bool {
	return t.raw == t.word && usableWord(t)
}

// innerToken is plainToken allowing a trailing comma, for a word a window
// goes on past.
func innerToken(t textToken) bool {
	return strings.TrimSuffix(t.raw, ",") == t.word && usableWord(t)
}

// lastToken is plainToken allowing a trailing comma, colon or stop, for the
// word a window ends with.
func lastToken(t textToken) bool {
	return strings.TrimRight(t.raw, ",.;:!?") == t.word && usableWord(t)
}

func usableWord(t textToken) bool {
	return !t.code && answerable(t.word) && !entryIDWord.MatchString(strings.ToLower(t.word))
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

// phraseAt returns the phrase of the tokens of line from i on, when they are
// plain, single-spaced, occur once in the text and not all are function
// words.
func (p questionText) phraseAt(line []textToken, i int) (string, bool) {
	if i < 0 || i+phraseWords > len(line) {
		return "", false
	}
	tokens := line[i : i+phraseWords]
	if slices.ContainsFunc(tokens, func(t textToken) bool { return !plainToken(t) }) {
		return "", false
	}
	phrase, ok := p.run(tokens)
	if !ok || strings.Count(p.lower, strings.ToLower(phrase)) != 1 {
		return "", false
	}
	return phrase, slices.ContainsFunc(tokens, func(t textToken) bool { return !functionWords[strings.ToLower(t.word)] })
}

// run joins tokens with single spaces when that is how the text holds them.
func (p questionText) run(tokens []textToken) (string, bool) {
	raws := make([]string, len(tokens))
	for i, t := range tokens {
		raws[i] = t.raw
	}
	joined := strings.Join(raws, " ")
	return joined, p.text[tokens[0].start:tokens[len(tokens)-1].end] == joined
}

// window reports whether tokens read to an agent as the parser reads them:
// single-spaced words, nothing it might count as a word between them, and
// no stop before the last.
func (p questionText) window(tokens []textToken) bool {
	for _, t := range tokens[:len(tokens)-1] {
		if !innerToken(t) {
			return false
		}
	}
	_, ok := p.run(tokens)
	return ok && lastToken(tokens[len(tokens)-1])
}

func answerOf(tokens ...textToken) string {
	words := make([]string, len(tokens))
	for i, t := range tokens {
		words[i] = t.word
	}
	return strings.Join(words, " ")
}

// candidates lists every question of kind the text holds, each a window of
// one line: the phrase and the words asked for side by side.
func (p questionText) candidates(kind QuestionKind) []TextQuestion {
	var out []TextQuestion
	for _, line := range p.lines {
		n := len(line)
		for i := range line {
			phrase, ok := p.phraseAt(line, i)
			if !ok {
				continue
			}
			end := i + phraseWords
			var window, asked []textToken
			q := TextQuestion{Kind: kind, Phrase: phrase}
			switch kind {
			case AskWordsAfter:
				if end+2 > n {
					continue
				}
				window, asked = line[i:end+2], line[end:end+2]
			case AskWordsBefore:
				if i < 2 {
					continue
				}
				window, asked = line[i-2:end], line[i-2:i]
			case AskNumberAfter:
				if end >= n || !numberWord(line[end].word) {
					continue
				}
				window, asked = line[i:end+1], line[end:end+1]
			case AskBetween:
				second, found := p.phraseAt(line, end+1)
				if !found {
					continue
				}
				q.Second = second
				window, asked = line[i:end+1+phraseWords], line[end:end+1]
			}
			// A run ending on a stop word invites completing the phrase past it.
			if !p.window(window) || len(asked) > 1 && functionWords[strings.ToLower(asked[len(asked)-1].word)] {
				continue
			}
			q.Answer = answerOf(asked...)
			if len(q.Ask()) > maxAskChars {
				continue
			}
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
