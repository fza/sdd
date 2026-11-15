package model

// ReadableVersion picks the one version of an entry a search reads while a
// re-embed under a new rune cap is pending. candidates are the entry's state
// hashes, one per read cap, newest cap first. The newest published candidate
// wins, so a re-embedded entry never surfaces its old version beside the new
// one; the oldest candidate is the fallback when no newer one is published.
func ReadableVersion(candidates []string, published map[string]bool) string {
	if len(candidates) == 0 {
		return ""
	}
	for _, candidate := range candidates[:len(candidates)-1] {
		if published[candidate] {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}
