package testsupport

import (
	"regexp"
	"strings"
	"unicode"
)

// questionLine reads a text question as sdd ends a response with it.
var questionLine = regexp.MustCompile(`q (.+?): (.+?) \| lost`)

var questionForms = []struct {
	form   *regexp.Regexp
	answer func(text string, phrases []string) (string, bool)
}{
	{regexp.MustCompile(`^word between "(.+)" and "(.+)"\?$`), func(text string, p []string) (string, bool) {
		at := strings.Index(text, p[0]+" ")
		if at < 0 {
			return "", false
		}
		rest := text[at+len(p[0])+1:]
		word, _, ok := strings.Cut(rest, " "+p[1])
		return strings.TrimRight(word, ","), ok && !strings.Contains(word, " ")
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

// Question is a text question a served result ends with: the name of the
// text it asks about and the question without it.
type Question struct {
	Name string
	Ask  string
}

// Questions returns the text questions in s, a served result, in order.
func Questions(s string) []Question {
	var out []Question
	for _, found := range questionLine.FindAllStringSubmatch(s, -1) {
		out = append(out, Question{Name: found[1], Ask: found[2]})
	}
	return out
}

// AnswerAll answers the questions of s, a served result's question field, as
// an agent holding texts does: in order, parted by " ; ", lost for one no
// text answers. It reports false when s holds no question or one is lost.
func AnswerAll(s string, texts []string) (string, bool) {
	questions := Questions(s)
	if len(questions) == 0 {
		return "", false
	}
	answers := make([]string, len(questions))
	all := true
	for i, q := range questions {
		words, ok := AnswerQuestion(q.Ask, texts)
		if !ok {
			words, all = "lost", false
		}
		answers[i] = words
	}
	return strings.Join(answers, " ; "), all
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

// wordsAfter lists the first n words of s, which starts inside a line, where
// a number such as the "1." ending a sentence is a word, not a list marker.
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
