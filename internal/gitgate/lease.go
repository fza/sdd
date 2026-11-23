package gitgate

import (
	"context"
	"time"
)

// Demand is what a renewal of a repository's lease asks for.
type Demand struct {
	// Sync is set when the renewal started the lease and the repository's
	// effective fetch cooldown has passed: one sync runs now.
	Sync bool
	// Wait is how long a call waits for that sync (read_wait).
	Wait time.Duration
	// Fetched is when the repository's last fetch succeeded; zero when none
	// did.
	Fetched time.Time
}

// Demand starts or renews target's lease, which holds lease_pause after its
// last renewal for every process sharing the record.
func (g *Gate) Demand(ctx context.Context, target Target) (Demand, error) {
	limits, err := g.limitsFor(target)
	if err != nil {
		return Demand{}, err
	}
	unlock, err := g.store.lock(ctx, false)
	if err != nil {
		return Demand{}, err
	}
	defer unlock()
	l, err := g.read(ctx)
	if err != nil {
		return Demand{}, err
	}
	now := g.now()
	repo := l.repo(target)
	started := !leaseHeld(repo, limits, now)
	repo.Lease = now.UTC()
	demand := Demand{Wait: limits.ReadWait}
	if repo.Fetch != nil {
		demand.Fetched = repo.Fetch.LastSuccess
	}
	if started {
		demand.Sync = target.Protocol == Local || evaluate(l, target, Fetch, limits, now, false).due
	}
	err = g.store.write(l, now)
	if err != nil {
		return Demand{}, err
	}
	return demand, nil
}

// Leased reports whether target's lease holds now.
func (g *Gate) Leased(ctx context.Context, target Target) (bool, error) {
	limits, err := g.limitsFor(target)
	if err != nil {
		return false, err
	}
	unlock, err := g.store.lock(ctx, true)
	if err != nil {
		return false, err
	}
	defer unlock()
	l, err := g.store.read()
	if err != nil {
		return false, err
	}
	repo := l.Repos[target.Repo]
	return repo != nil && leaseHeld(repo, limits, g.now()), nil
}

func leaseHeld(repo *repoRecord, limits Limits, now time.Time) bool {
	return !repo.Lease.IsZero() && now.Before(repo.Lease.Add(limits.LeasePause))
}
