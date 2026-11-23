package llmops

// QuoteLocatesInAny reports whether quote locates a passage of text that
// overlaps one of passages.
func QuoteLocatesInAny(text, quote string, passages []string) bool {
	for _, passage := range passages {
		if QuoteLocatesIn(text, quote, passage) {
			return true
		}
	}
	return false
}
