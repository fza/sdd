package model

import "strings"

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
