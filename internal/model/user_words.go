package model

import (
	"slices"
	"strings"
	"unicode"
)

// UserWordsOwed reports which of the user's own words a draft of kind k
// carries when a user takes part in its capture: every decision states the
// user's reason, and a directive also the user's intent. Signals owe none.
func UserWordsOwed(k Kind) (reason, intent bool) {
	if !decisionKinds[k] {
		return false, false
	}
	return true, k == KindDirective
}

// ReasonRepeatsIntent reports whether a decision's reason only restates the
// directive's intent: one holds the other once case, spacing and closing
// punctuation are set aside. An empty side repeats nothing.
func ReasonRepeatsIntent(reason, intent string) bool {
	r, i := comparableWords(reason), comparableWords(intent)
	if r == "" || i == "" {
		return false
	}
	return strings.Contains(r, i) || strings.Contains(i, r)
}

func comparableWords(s string) string {
	return strings.TrimRight(strings.Join(strings.Fields(strings.ToLower(s)), " "), ".,;:!?")
}

// QuotedFrom reports whether words quote message: message holds them as
// whole words in that order, case and punctuation set aside.
func QuotedFrom(words, message string) bool {
	w := wordRun(words)
	return len(w) > 0 && indexWords(wordRun(message), w) >= 0
}

// wordRun splits s into its lowercased runs of letters and digits.
func wordRun(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// indexWords returns where sub starts in words as a contiguous run, or -1.
func indexWords(words, sub []string) int {
	for at := 0; at+len(sub) <= len(words); at++ {
		if slices.Equal(words[at:at+len(sub)], sub) {
			return at
		}
	}
	return -1
}
