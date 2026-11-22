package test

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// questionLine reads a text question as sdd ends a response with it.
var questionLine = regexp.MustCompile(`q (.+?): (.+?) \| lost`)

var questionForms = []struct {
	form   *regexp.Regexp
	answer func(text string, phrases []string) (string, bool)
}{
	{regexp.MustCompile(`^first 2 words of the sentence after the one with "(.+)"\?$`), func(text string, p []string) (string, bool) {
		_, end, ok := sentenceAround(text, p[0])
		if !ok {
			return "", false
		}
		return firstWords(text[end:], 2)
	}},
	{regexp.MustCompile(`^first 2 words of the sentence with "(.+)"\?$`), func(text string, p []string) (string, bool) {
		start, end, ok := sentenceAround(text, p[0])
		if !ok {
			return "", false
		}
		return firstWords(text[start:end], 2)
	}},
	{regexp.MustCompile(`^last 2 words of the sentence with "(.+)"\?$`), func(text string, p []string) (string, bool) {
		start, end, ok := sentenceAround(text, p[0])
		if !ok {
			return "", false
		}
		return lastWords(text[start:end], 2)
	}},
	{regexp.MustCompile(`^word between "(.+)" and "(.+)"\?$`), func(text string, p []string) (string, bool) {
		at := strings.Index(text, p[0]+" ")
		if at < 0 {
			return "", false
		}
		rest := text[at+len(p[0])+1:]
		word, _, ok := strings.Cut(rest, " "+p[1])
		return word, ok && !strings.Contains(word, " ")
	}},
	{regexp.MustCompile(`^number after "(.+)"\?$`), func(text string, p []string) (string, bool) {
		at := strings.Index(text, p[0])
		if at < 0 {
			return "", false
		}
		return wordsAfter(text[at+len(p[0]):], 1)
	}},
	{regexp.MustCompile(`^2 words after "(.+)"\?$`), func(text string, p []string) (string, bool) {
		at := strings.Index(text, p[0])
		if at < 0 {
			return "", false
		}
		return wordsAfter(text[at+len(p[0]):], 2)
	}},
	{regexp.MustCompile(`^2 words before "(.+)"\?$`), func(text string, p []string) (string, bool) {
		at := strings.Index(text, p[0])
		if at < 0 {
			return "", false
		}
		return lastWords(text[:at], 2)
	}},
}

// LastQuestion returns the last text question in s, a served result, with
// the name of the text it asks about.
func LastQuestion(s string) (name, ask string, ok bool) {
	found := questionLine.FindAllStringSubmatch(s, -1)
	if len(found) == 0 {
		return "", "", false
	}
	last := found[len(found)-1]
	return last[1], last[2], true
}

// AnswerQuestion answers ask, a text question without its name, as an agent
// holding texts looks it up: in the one text holding its phrase. It reports
// false when no text holds it, where the agent answers lost.
func AnswerQuestion(ask string, texts []string) (string, bool) {
	for _, form := range questionForms {
		m := form.form.FindStringSubmatch(ask)
		if m == nil {
			continue
		}
		for _, text := range texts {
			if !strings.Contains(text, m[1]) {
				continue
			}
			if answer, ok := form.answer(text, m[1:]); ok {
				return answer, true
			}
		}
		return "", false
	}
	return "", false
}

// sentenceAround returns the bounds of the sentence of text holding phrase:
// a sentence ends at a full stop, question or exclamation mark before a
// space, or at a line break unless the next line goes on in lower case.
func sentenceAround(text, phrase string) (int, int, bool) {
	at := strings.Index(text, phrase)
	if at < 0 {
		return 0, 0, false
	}
	start := 0
	for i := at - 1; i >= 0; i-- {
		if boundaryAfter(text, i) {
			start = i + 1
			break
		}
	}
	end := len(text)
	for i := at + len(phrase); i < len(text); i++ {
		if boundaryAfter(text, i) {
			end = i + 1
			break
		}
	}
	return start, end, true
}

func boundaryAfter(text string, i int) bool {
	switch text[i] {
	case '\n':
		next, _ := utf8.DecodeRuneInString(strings.TrimLeft(text[i+1:], " \t"))
		return !unicode.IsLower(next)
	case '.', '!', '?':
		rest := strings.TrimLeft(text[i+1:], ")]\"'*_`")
		token := text[strings.LastIndexAny(text[:i], " \n")+1 : i]
		abbreviation := strings.Contains(strings.TrimLeft(token, "([\"'*_`"), ".")
		return !abbreviation && (rest == "" || rest[0] == ' ' || rest[0] == '\n')
	}
	return false
}

func firstWords(s string, n int) (string, bool) {
	words := wordsOf(s)
	if len(words) < n {
		return "", false
	}
	return strings.Join(words[:n], " "), true
}

// wordsAfter is firstWords for s that starts inside a line, where a number
// such as the "1." ending a sentence is a word, not a list marker.
func wordsAfter(s string, n int) (string, bool) {
	line, rest, _ := strings.Cut(s, "\n")
	var words []string
	for _, field := range strings.Fields(line) {
		if word := strings.TrimFunc(field, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }); word != "" {
			words = append(words, word)
		}
	}
	words = append(words, wordsOf(rest)...)
	if len(words) < n {
		return "", false
	}
	return strings.Join(words[:n], " "), true
}

func lastWords(s string, n int) (string, bool) {
	words := wordsOf(s)
	if len(words) < n {
		return "", false
	}
	return strings.Join(words[len(words)-n:], " "), true
}

// wordsOf lists the words of s, punctuation and list markers left out.
func wordsOf(s string) []string {
	var words []string
	for line := range strings.SplitSeq(s, "\n") {
		for i, field := range strings.Fields(line) {
			if i == 0 && listMarker(field) {
				continue
			}
			word := strings.TrimFunc(field, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
			if word != "" {
				words = append(words, word)
			}
		}
	}
	return words
}

func listMarker(field string) bool {
	switch field {
	case "-", "*", "+", ">":
		return true
	}
	digits := strings.TrimRight(field, ".)")
	return digits != field && digits != "" && strings.Trim(digits, "0123456789") == ""
}
