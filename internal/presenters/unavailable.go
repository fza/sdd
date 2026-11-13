package presenters

import "time"

// unavailableMarker is the one marker every surface renders in place of a
// ref target that does not resolve, with the short reason when one is known
// (20261113-235859-d-cpt-l3m).
func unavailableMarker(repo, reason string) string {
	if reason == "" {
		return "[unresolved: repo " + repo + "]"
	}
	return "[unresolved: repo " + repo + ": " + reason + "]"
}

// cloneAge marks a ref target read from the clone of a repo beyond the
// direct dependencies with when that clone last fetched; empty for every
// other target.
func cloneAge(fetched time.Time) string {
	if fetched.IsZero() {
		return ""
	}
	return "[clone as of " + fetched.Local().Format(time.DateTime) + "]"
}
