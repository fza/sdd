package gitgate

import (
	"context"
	"slices"
	"strings"
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

// fetchSlice is target's fetch cooldown: the configured one, stretched to the
// slice of its bucket's hourly limit each repository the ledger tracks on its
// host and protocol gets. A repository counting against both HTTPS buckets
// takes the smaller limit.
func (l *ledger) fetchSlice(repo *repoRecord, target Target, limits Limits) time.Duration {
	buckets := bucketsFor(repo, target, limits)
	perHour := buckets[0].limit
	for _, b := range buckets[1:] {
		perHour = min(perHour, b.limit)
	}
	return model.GitFetchSlice(limits.FetchCooldown, perHour, l.tracked(target.Host, target.Protocol))
}

func (l *ledger) tracked(host string, protocol Protocol) int {
	count := 0
	for key, repo := range l.Repos {
		if repo.Protocol == protocol && repoHost(key) == host {
			count++
		}
	}
	return count
}

func repoHost(key string) string {
	host, _, _ := strings.Cut(key, "/")
	return host
}

// accrue credits the wallets of host's buckets with every fetch slice its
// repositories left unused, rolling each repository's slice forward.
func (l *ledger) accrue(host string, limits Limits, now time.Time) {
	since := windowStart(now)
	for key, repo := range l.Repos {
		if repo.Fetch == nil || repoHost(key) != host || (repo.Protocol != SSH && repo.Protocol != HTTPS) {
			continue
		}
		target := Target{Host: host, Repo: key, Protocol: repo.Protocol}
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
