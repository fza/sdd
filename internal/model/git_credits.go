package model

import "time"

// gitFetchPercent is the share of a bucket's hourly limit automatic fetches
// may use; the rest is kept for pushes and commanded syncs.
const gitFetchPercent = 90

// GitFetchShare is how many operations of a bucket's hourly limit perHour
// automatic fetches may use.
func GitFetchShare(perHour int) int {
	return (perHour*gitFetchPercent + 99) / 100
}

// GitFetchSlice is the fetch cooldown of each of repos repositories sharing
// an hourly bucket of perHour operations: the configured cooldown, stretched
// so that one fetch per repository per slice stays within the fetch share of
// perHour.
func GitFetchSlice(configured time.Duration, perHour, repos int) time.Duration {
	if perHour <= 0 || repos <= 0 {
		return configured
	}
	return max(configured, time.Hour*time.Duration(repos)*100/(time.Duration(perHour)*gitFetchPercent))
}

// IdleGitSlices rolls a repository's fetch slice, which came due at due,
// forward by period up to now. Each slice that ended without a fetch earns
// one credit, dated when it came due; credits dated before since are not
// returned. current is when the slice in progress came due.
func IdleGitSlices(due time.Time, period time.Duration, since, now time.Time) (credits []time.Time, current time.Time) {
	if due.IsZero() || period <= 0 {
		return nil, due
	}
	if due.Before(since) {
		due = due.Add(since.Sub(due) / period * period)
	}
	for !due.Add(period).After(now) {
		if !due.Before(since) {
			credits = append(credits, due)
		}
		due = due.Add(period)
	}
	return credits, due
}
