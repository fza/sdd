package gitgate

import (
	"context"
	"slices"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

type creditsKey struct{}

// WithCredits lets an operation of kinds that Run admits under ctx spend a
// credit inside its cooldown. Only a push of commits waiting and a sync the
// user commands do; an automatic read or round never does.
func WithCredits(ctx context.Context, kinds ...Kind) context.Context {
	current, _ := ctx.Value(creditsKey{}).([]Kind)
	merged := slices.Clone(current)
	for _, kind := range kinds {
		if !slices.Contains(merged, kind) {
			merged = append(merged, kind)
		}
	}
	return context.WithValue(ctx, creditsKey{}, merged)
}

func creditsFor(ctx context.Context, kind Kind) bool {
	kinds, _ := ctx.Value(creditsKey{}).([]Kind)
	return slices.Contains(kinds, kind)
}

// creditFloor is the least time between two operations of kind on one
// repository, credit or not.
func creditFloor(kind Kind) time.Duration {
	if kind == Push {
		return model.MinGitPushCooldown
	}
	return model.MinGitFetchCooldown
}

// fetchSlice is target's effective fetch cooldown: the configured one,
// stretched to the slice of each of its buckets' fetch share that every
// sdd-owned clone in that bucket gets; the longest wins.
func (l *ledger) fetchSlice(repo *repoRecord, target Target, limits Limits) time.Duration {
	slice := limits.FetchCooldown
	for _, b := range bucketsFor(repo, target, limits) {
		slice = max(slice, model.GitFetchSlice(limits.FetchCooldown, b.limit, l.clonesIn(b.key, limits)))
	}
	return slice
}

// clonesIn counts the sdd-owned clones counting against the bucket key; a
// clone whose login is not known yet counts in both HTTPS buckets.
func (l *ledger) clonesIn(key string, limits Limits) int {
	count := 0
	for _, clone := range l.clones {
		repo := l.Repos[clone.Repo]
		if repo == nil {
			repo = &repoRecord{}
		}
		for _, b := range bucketsFor(repo, clone, limits) {
			if b.key == key {
				count++
			}
		}
	}
	return count
}

// accrue credits the wallets of host's buckets with every fetch slice its
// sdd-owned clones left unused, rolling each clone's slice forward.
func (l *ledger) accrue(host string, limits Limits, now time.Time) {
	since := windowStart(now)
	for _, target := range l.clones {
		repo := l.Repos[target.Repo]
		if target.Host != host || repo == nil || repo.Fetch == nil {
			continue
		}
		due := repo.Fetch.Slice
		if due.IsZero() {
			due = repo.Fetch.Next
		}
		credits, current := model.IdleGitSlices(due, l.fetchSlice(repo, target, limits), since, now)
		for _, at := range credits {
			for _, b := range bucketsFor(repo, target, limits) {
				l.Earned.add(b.key, unixMinute(at), 1)
			}
		}
		repo.Fetch.Slice = current
	}
}

// hasCredit reports whether every bucket target counts against holds a
// credit.
func (l *ledger) hasCredit(repo *repoRecord, target Target, limits Limits, now time.Time) bool {
	for _, b := range bucketsFor(repo, target, limits) {
		if l.balance(b.key, now) < 1 {
			return false
		}
	}
	return true
}

// cooldown is how long target waits after an operation of kind.
func (l *ledger) cooldown(repo *repoRecord, target Target, kind Kind, limits Limits) time.Duration {
	if kind == Push {
		return limits.PushCooldown
	}
	return l.fetchSlice(repo, target, limits)
}

// setNext sets when record's kind is next allowed; a fetch's next slice comes
// due then too.
func setNext(record *kindRecord, kind Kind, next time.Time) {
	record.Next = next.UTC()
	if kind == Fetch {
		record.Slice = record.Next
	}
}
