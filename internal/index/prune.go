package index

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// Background pruning of index stores (20261101-131514-d-cpt-9ci). A pass
// takes a store's lock only when it is free, holds it for at most pruneBatch
// versions, and then pauses before the next session: flock gives a waiting
// reader no precedence, so without the pause a pass retaking the lock at once
// starves every search polling for it (measured: a 460 ms wait while 1500
// versions went in back-to-back sessions).
const (
	// pruneBatch caps the versions one lock session drops. A session's
	// length is mostly the manifest save and its sync, not the versions, so
	// a smaller batch only adds sessions: on a loaded machine 25 per session
	// stalled one search 86 ms against 45 ms at 100.
	pruneBatch = 100
	// prunePause lets readers polling for the lock (every
	// boundedRetryInterval) in between two sessions.
	prunePause = 50 * time.Millisecond
	// pruneResumeWait bounds how long a pass already under way waits for
	// readers to let go before leaving the rest to the next pass.
	pruneResumeWait = 2 * time.Second
)

// prunedSuffix marks a store directory renamed for removal, so a removal a
// crash interrupted is finished by the next run.
const prunedSuffix = ".pruned"

// PruneVersions removes the versions background pruning deletes and returns
// their chunk IDs with the count removed, at most limit versions (no limit
// when limit is zero). A current-rule version goes once a newer version of the
// same entry under the same rule has been in the store for longer than
// retention, unless keep names its hash as the entry's current state. Another
// rule's versions go once that rule's latest use, its stamp in ruleUses or its
// newest write, is older than retention. Chunk IDs a kept version still names
// are never returned. It performs no I/O.
func (m *Manifest) PruneVersions(now time.Time, retention time.Duration, ruleUses map[string]time.Time, keep map[string]string, limit int) (chunkIDs []string, versions int) {
	ruleLast := map[string]time.Time{}
	for _, state := range m.Entries {
		for _, v := range state.Versions {
			d := v.derivation()
			if d != DerivationCurrent && v.IndexedAt.After(ruleLast[d]) {
				ruleLast[d] = v.IndexedAt
			}
		}
	}
	for d, at := range ruleUses {
		if at.After(ruleLast[d]) {
			ruleLast[d] = at
		}
	}
	expired := func(id string, state EntryState, v EntryVersion) bool {
		d := v.derivation()
		if d != DerivationCurrent {
			return now.Sub(ruleLast[d]) > retention
		}
		if keep[id] == v.Hash {
			return false
		}
		var replacedAt time.Time
		for _, other := range state.Versions {
			if other.derivation() != d || !other.IndexedAt.After(v.IndexedAt) {
				continue
			}
			if replacedAt.IsZero() || other.IndexedAt.Before(replacedAt) {
				replacedAt = other.IndexedAt
			}
		}
		return !replacedAt.IsZero() && now.Sub(replacedAt) > retention
	}

	var dropped []string
	for _, id := range m.EntryIDsSorted() {
		if limit > 0 && versions >= limit {
			break
		}
		state := m.Entries[id]
		kept := state.Versions[:0:0]
		for _, v := range state.Versions {
			if (limit == 0 || versions < limit) && expired(id, state, v) {
				dropped = append(dropped, v.ChunkIDs...)
				versions++
				continue
			}
			kept = append(kept, v)
		}
		switch {
		case len(kept) == 0:
			delete(m.Entries, id)
		case len(kept) != len(state.Versions):
			m.Entries[id] = EntryState{Versions: kept}
		}
	}
	if versions == 0 {
		return nil, 0
	}
	named := map[string]bool{}
	for _, state := range m.Entries {
		for _, id := range state.AllChunkIDs() {
			named[id] = true
		}
	}
	for _, id := range dropped {
		if !named[id] {
			chunkIDs = append(chunkIDs, id)
		}
	}
	slices.Sort(chunkIDs)
	return slices.Compact(chunkIDs), versions
}

// PrunedVersions is what PruneStore removed from one store.
type PrunedVersions struct {
	Versions int
	Bytes    int64
}

// PruneStore deletes the versions PruneVersions selects from the store at
// indexDir, in lock sessions of at most pruneBatch versions. It returns
// ErrLockBusy when another process holds the store at the start, or longer
// than pruneResumeWait between sessions, with what the sessions before it
// removed. The manifest is saved before rows are deleted, so it never names a
// row that is gone.
func PruneStore(ctx context.Context, indexDir string, now time.Time, retention time.Duration, keep map[string]string) (PrunedVersions, error) {
	var total PrunedVersions
	var wait time.Duration
	for {
		pruned, more, err := pruneSession(ctx, indexDir, now, retention, keep, wait)
		total.Versions += pruned.Versions
		total.Bytes += pruned.Bytes
		if err != nil || !more {
			return total, err
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(prunePause):
		}
		wait = pruneResumeWait
	}
}

func pruneSession(ctx context.Context, indexDir string, now time.Time, retention time.Duration, keep map[string]string, wait time.Duration) (_ PrunedVersions, more bool, err error) {
	lock := lockFile(indexDir)
	locked, err := lock.TryLock()
	if err == nil && !locked && wait > 0 {
		waitCtx, cancel := context.WithTimeout(ctx, wait)
		locked, err = lock.TryLockContext(waitCtx, prunePause)
		cancel()
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			locked, err = false, nil
		}
	}
	if err != nil {
		return PrunedVersions{}, false, fmt.Errorf("acquiring index write lock at %s: %w", indexDir, err)
	}
	if !locked {
		return PrunedVersions{}, false, ErrLockBusy
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	manifest, err := LoadManifest(indexDir)
	if err != nil {
		return PrunedVersions{}, false, err
	}
	uses, err := RuleLastUses(indexDir)
	if err != nil {
		return PrunedVersions{}, false, err
	}
	chunkIDs, versions := manifest.PruneVersions(now, retention, uses, keep, pruneBatch)
	if versions == 0 {
		return PrunedVersions{}, false, nil
	}
	pruned := PrunedVersions{Versions: versions, Bytes: DocumentsSize(indexDir, chunkIDs)}
	if err := manifest.Save(indexDir); err != nil {
		return PrunedVersions{}, false, fmt.Errorf("save manifest after prune: %w", err)
	}
	for _, id := range chunkIDs {
		if err := os.Remove(documentPath(indexDir, id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return pruned, false, fmt.Errorf("deleting index row: %w", err)
		}
	}
	if err := bumpGeneration(indexDir); err != nil {
		return pruned, false, fmt.Errorf("bumping store generation at %s: %w", indexDir, err)
	}
	return pruned, versions == pruneBatch, nil
}

// RemoveStore deletes the whole store at indexDir when it is still unused for
// longer than retention under its locks, and reports the bytes freed. It
// returns ErrLockBusy when another process holds either lock, and zero when a
// use arrived since the caller looked.
func RemoveStore(indexDir string, now time.Time, retention time.Duration) (int64, error) {
	doomed, err := retireStore(indexDir, now, retention)
	if err != nil || doomed == "" {
		return 0, err
	}
	size, err := StoreSize(doomed)
	if err != nil {
		return 0, err
	}
	if err := os.RemoveAll(doomed); err != nil {
		return 0, fmt.Errorf("removing %s: %w", doomed, err)
	}
	return size, nil
}

// retireStore moves the store's contents out under both locks and returns
// where they went, or "" when a use arrived since the caller looked. The
// directory and its lock files stay: a process waiting on a lock holds an
// open path into it, and finds an empty store, which it fills afresh, instead
// of a lock path whose directory is gone.
func retireStore(indexDir string, now time.Time, retention time.Duration) (doomed string, err error) {
	for _, l := range []*flock.Flock{lockFile(indexDir), fillLockFile(indexDir)} {
		locked, lockErr := l.TryLock()
		if lockErr != nil {
			return "", fmt.Errorf("locking index store %s: %w", indexDir, lockErr)
		}
		if !locked {
			return "", ErrLockBusy
		}
		defer func() { err = errors.Join(err, l.Unlock()) }()
	}
	last, err := StoreLastUse(indexDir)
	if err != nil {
		return "", err
	}
	if now.Sub(last) <= retention {
		return "", nil
	}
	doomed = filepath.Join(filepath.Dir(indexDir), "."+filepath.Base(indexDir)+prunedSuffix)
	if err := os.RemoveAll(doomed); err != nil {
		return "", fmt.Errorf("removing %s: %w", doomed, err)
	}
	if err := os.Mkdir(doomed, 0o755); err != nil {
		return "", fmt.Errorf("retiring index store %s: %w", indexDir, err)
	}
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		return "", fmt.Errorf("retiring index store %s: %w", indexDir, err)
	}
	for _, entry := range entries {
		if entry.Name() == ".lock" || entry.Name() == ".fill.lock" {
			continue
		}
		if err := os.Rename(filepath.Join(indexDir, entry.Name()), filepath.Join(doomed, entry.Name())); err != nil {
			return "", fmt.Errorf("retiring index store %s: %w", indexDir, err)
		}
	}
	return doomed, nil
}

// Stores lists the store directories under cacheRoot's index tree, and the
// leftovers of removals a crash interrupted, which callers delete.
func Stores(cacheRoot string) (stores, leftovers []string, err error) {
	root := filepath.Join(cacheRoot, storeSubdir)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == root {
				return filepath.SkipAll
			}
			return err
		}
		if !d.IsDir() || path == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if strings.HasSuffix(d.Name(), prunedSuffix) {
				leftovers = append(leftovers, path)
			}
			return filepath.SkipDir
		}
		if isStoreDir(path) {
			stores = append(stores, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("listing index stores under %s: %w", root, err)
	}
	return stores, leftovers, nil
}

func isStoreDir(path string) bool {
	for _, marker := range []string{manifestPath(path), filepath.Join(path, "chromem")} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}
