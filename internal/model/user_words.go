package model

// UserWordsOwed reports which of the user's own words a draft of kind k
// carries when a user takes part in its capture: every decision states the
// user's reason, and a directive also the user's intent. Signals owe none.
func UserWordsOwed(k Kind) (reason, intent bool) {
	if !decisionKinds[k] {
		return false, false
	}
	return true, k == KindDirective
}
