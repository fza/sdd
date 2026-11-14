package gitgate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// PruneRecords drops the records of repositories the gate has neither admitted
// an operation for nor leased within retention, and whose next allowed times
// have passed, and reports how many went (20261114-095140-d-cpt-ad4).
func (g *Gate) PruneRecords(ctx context.Context, now time.Time, retention time.Duration) (int, error) {
	unlock, err := g.store.lock(ctx, false)
	if err != nil {
		return 0, err
	}
	defer unlock()
	l, err := g.store.read()
	if err != nil {
		return 0, err
	}
	removed := 0
	for repo, record := range l.Repos {
		if now.Sub(record.lastActivity()) > retention {
			delete(l.Repos, repo)
			removed++
		}
	}
	if removed == 0 {
		return 0, nil
	}
	return removed, g.store.write(l, now)
}

// lastActivity is the latest time the record names, a pending next allowed
// time included.
func (r *repoRecord) lastActivity() time.Time {
	last := r.Lease
	for _, kind := range []*kindRecord{r.Fetch, r.Push} {
		if kind == nil {
			continue
		}
		for _, at := range []time.Time{kind.LastAttempt, kind.LastSuccess, kind.FailedAt, kind.RetryAt, kind.Next, kind.Slice} {
			if at.After(last) {
				last = at
			}
		}
	}
	return last
}

// SweepTraces removes trace files older than minAge, which an operation a
// crash ended left behind, and reports how many went.
func (g *Gate) SweepTraces(now time.Time, minAge time.Duration) (int, error) {
	dir := filepath.Join(g.stateDir, "traces")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("listing git traces: %w", err)
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, fmt.Errorf("reading git trace: %w", err)
		}
		if now.Sub(info.ModTime()) <= minAge {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, fmt.Errorf("removing git trace: %w", err)
		}
		removed++
	}
	return removed, nil
}
