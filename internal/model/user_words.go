package model

import (
	"slices"
	"strings"
	"unicode"
)

// UserWordsOwed reports which words a draft of kind k owes in every dialogue
// mode, the user's or the agent's: a directive and a plan state a reason, and
// a directive also its intent. Any other decision may state a reason; signals
// carry none.
func UserWordsOwed(k Kind) (reason, intent bool) {
	return k == KindDirective || k == KindPlan, k == KindDirective
}

// causeWords open a clause that states why, so a reason holding the intent
// beside one of them gives a cause of its own.
var causeWords = []string{"because", "since", "so", "otherwise", "due", "given"}

// ReasonRepeatsIntent reports whether a decision's reason only restates the
// directive's intent: one holds the other word for word, case and
// punctuation set aside, and the words the reason adds open no cause. An
// empty side repeats nothing.
func ReasonRepeatsIntent(reason, intent string) bool {
	r, i := wordRun(reason), wordRun(intent)
	if len(r) == 0 || len(i) == 0 {
		return false
	}
	if indexWords(i, r) >= 0 {
		return true
	}
	at := indexWords(r, i)
	if at < 0 {
		return false
	}
	added := append(slices.Clone(r[:at]), r[at+len(i):]...)
	return !slices.ContainsFunc(added, func(w string) bool { return slices.Contains(causeWords, w) })
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
