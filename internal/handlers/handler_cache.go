package handlers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/graphcache"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/llm/embed"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// CurrentHashSource supplies the current state hash of each entry in the
// checkout a pruning run starts from, whose current versions it never deletes.
type CurrentHashSource interface {
	CurrentHashes(ctx context.Context, maxRunes int) (map[string]string, error)
}

// CacheHandlerOptions configures NewCacheHandler. CacheRoot, StateRoot and
// Registry are required; Keep is nil outside an sdd checkout; Now defaults to
// time.Now.
type CacheHandlerOptions struct {
	CacheRoot string
	StateRoot string
	Registry  *repos.Registry
	Keep      CurrentHashSource
	Now       func() time.Time
}

// CacheHandler runs the machine-global cache pruning.
type CacheHandler struct {
	cacheRoot string
	stateRoot string
	registry  *repos.Registry
	keep      CurrentHashSource
	now       func() time.Time
}

func NewCacheHandler(opts CacheHandlerOptions) *CacheHandler {
	h := &CacheHandler{cacheRoot: opts.CacheRoot, stateRoot: opts.StateRoot, registry: opts.Registry, keep: opts.Keep, now: opts.Now}
	if h.now == nil {
		h.now = time.Now
	}
	return h
}

// ErrCachePruneRunning refuses a pass while another process runs one.
var ErrCachePruneRunning = errors.New("another cache pruning run is in progress")

// Prune runs one pruning pass over the whole cache and records its result in
// the pruning status, failure included, so sdd info shows it.
func (h *CacheHandler) Prune(ctx context.Context, cmd *command.PruneCacheCmd) (err error) {
	if err := os.MkdirAll(h.stateRoot, 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}
	run := flock.New(filepath.Join(h.stateRoot, cacheprune.RunLockFile))
	locked, err := run.TryLock()
	if err != nil {
		return fmt.Errorf("locking cache pruning: %w", err)
	}
	if !locked {
		return ErrCachePruneRunning
	}
	defer func() { err = errors.Join(err, run.Unlock()) }()

	now := h.now()
	if err := cacheprune.Update(ctx, h.stateRoot, func(s *model.CachePruneStatus) { s.StartedAt = now }); err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, model.CachePruneRunLimit)
	defer cancel()
	counts, runErr := h.prune(runCtx, now)
	if cmd.OnPruned != nil {
		cmd.OnPruned(counts)
	}
	recordErr := cacheprune.Update(context.WithoutCancel(ctx), h.stateRoot, func(s *model.CachePruneStatus) {
		s.FinishedAt = h.now()
		s.Removed = counts
		s.Error = ""
		if runErr != nil {
			s.Error = runErr.Error()
		}
	})
	return errors.Join(runErr, recordErr)
}

func (h *CacheHandler) prune(ctx context.Context, now time.Time) (model.CachePruneCounts, error) {
	var counts model.CachePruneCounts
	cfg, err := h.registry.Load()
	if err != nil {
		return counts, err
	}
	retention, err := model.ResolveCacheRetention(cfg.Cache)
	if err != nil {
		return counts, err
	}
	replaced, err := model.ResolveReplacedRetention(cfg.Index)
	if err != nil {
		return counts, err
	}
	// An entry's state hash depends on the rune cap a store was chunked
	// under, so the checkout's current versions are hashed once per cap.
	keepByCap := map[int]map[string]string{}
	keepFor := func(dir string) (map[string]string, error) {
		if h.keep == nil {
			return nil, nil
		}
		manifest, err := index.LoadManifest(dir)
		if err != nil {
			return nil, err
		}
		if keep, ok := keepByCap[manifest.MaxRunes]; ok {
			return keep, nil
		}
		keep, err := h.keep.CurrentHashes(ctx, manifest.MaxRunes)
		if err != nil {
			return nil, fmt.Errorf("reading the checkout's current entries: %w", err)
		}
		keepByCap[manifest.MaxRunes] = keep
		return keep, nil
	}
	log := slogutils.FromContext(ctx)
	var errs []error

	// The quick passes run first, so the index stores, which take longest,
	// are what the run limit leaves to the next run.
	snapshots, freed, err := graphcache.New(filepath.Join(h.cacheRoot, "graph")).Prune(now, retention)
	counts.Snapshots += snapshots
	counts.Bytes += freed
	if err != nil {
		errs = append(errs, fmt.Errorf("pruning graph snapshots: %w", err))
	}

	calibrations, err := embed.PruneCalibrations(filepath.Join(h.cacheRoot, embed.CalibrationDirName), now, retention)
	counts.Records += calibrations
	if err != nil {
		errs = append(errs, err)
	}

	clones, err := h.registry.UnconnectedClones()
	if err != nil {
		errs = append(errs, err)
	}
	for _, dir := range clones {
		retired, err := h.registry.RetireUnconnectedClone(dir)
		switch {
		case errors.Is(err, repos.ErrClonesBusy):
			log.Info("connected-repo clones in use, skipped")
			continue
		case errors.Is(err, repos.ErrCloneHoldsClones):
			log.Warn("clone kept", "clone", dir, "reason", err)
			continue
		case err != nil:
			errs = append(errs, err)
			continue
		case retired == "":
			continue
		}
		size, err := dirSize(retired)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.RemoveAll(retired); err != nil {
			errs = append(errs, fmt.Errorf("removing clone %s: %w", dir, err))
			continue
		}
		counts.Clones++
		counts.Bytes += size
		log.Info("removed unconnected clone", "clone", dir, "bytes", size)
	}

	errs = append(errs, h.pruneIndex(ctx, now, index.Retention{Unused: retention, Replaced: replaced}, keepFor, &counts)...)
	return counts, errors.Join(errs...)
}

// pruneIndex applies the index retention rules to every store, the longest
// pass, which the run limit cuts short first.
func (h *CacheHandler) pruneIndex(ctx context.Context, now time.Time, r index.Retention, keepFor func(string) (map[string]string, error), counts *model.CachePruneCounts) []error {
	log := slogutils.FromContext(ctx)
	var errs []error
	tree, err := index.Stores(h.cacheRoot)
	if err != nil {
		return []error{err}
	}
	for _, dir := range tree.Interrupted {
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", dir, err))
		}
	}
	for _, dir := range tree.Retired {
		removed, err := index.RemoveRetiredStore(dir, now, model.CacheLeftoverAge)
		switch {
		case errors.Is(err, index.ErrLockBusy):
			log.Info("retired index store in use, skipped", "store", dir)
		case err != nil:
			errs = append(errs, err)
		case removed:
			counts.Leftovers++
		}
	}

	lastUse := map[string]time.Time{}
	newestSibling := map[string]time.Time{}
	for _, dir := range tree.Stores {
		last, err := index.StoreLastUse(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		lastUse[dir] = last
		if repo := filepath.Dir(dir); last.After(newestSibling[repo]) {
			newestSibling[repo] = last
		}
	}
	for _, dir := range tree.Stores {
		if ctx.Err() != nil {
			log.Info("cache pruning stopped at its time limit; the next run continues")
			break
		}
		last, ok := lastUse[dir]
		if !ok {
			continue
		}
		// A store another store of its repository was used after is a
		// replaced embedding setup's: it waits out r.Replaced, not r.Unused,
		// and its versions are not pruned, since a manifest save would count
		// as its use.
		replacedSetup := newestSibling[filepath.Dir(dir)].After(last)
		window := r.Unused
		if replacedSetup {
			window = r.Replaced
		}
		if now.Sub(last) > window {
			freed, err := index.RemoveStore(dir, now, window)
			switch {
			case errors.Is(err, index.ErrLockBusy):
				log.Info("index store in use, skipped", "store", dir)
			case err != nil:
				errs = append(errs, err)
			case freed > 0:
				counts.Stores++
				counts.Bytes += freed
				log.Info("removed index store", "store", dir, "replaced_setup", replacedSetup, "bytes", freed)
			}
			continue
		}
		if replacedSetup {
			continue
		}
		keep, err := keepFor(dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("pruning %s: %w", dir, err))
			continue
		}
		pruned, err := index.PruneStore(ctx, dir, now, r, keep)
		counts.Versions += pruned.Versions
		counts.Bytes += pruned.Bytes
		switch {
		case errors.Is(err, index.ErrLockBusy):
			log.Info("index store in use, skipped", "store", dir)
			continue
		case err != nil && ctx.Err() != nil:
			log.Info("cache pruning stopped at its time limit; the next run continues", "store", dir)
			continue
		case err != nil:
			errs = append(errs, fmt.Errorf("pruning %s: %w", dir, err))
			continue
		case pruned.Versions > 0:
			log.Info("pruned index versions", "store", dir, "versions", pruned.Versions, "bytes", pruned.Bytes)
		}
		swept, err := index.SweepStore(dir, now, model.CacheLeftoverAge)
		counts.Leftovers += swept.OrphanRows + swept.Temps
		counts.Bytes += swept.Bytes
		switch {
		case errors.Is(err, index.ErrLockBusy):
			log.Info("index store in use, skipped", "store", dir)
		case err != nil:
			errs = append(errs, fmt.Errorf("sweeping %s: %w", dir, err))
		}
	}
	return errs
}

func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("measuring %s: %w", dir, err)
	}
	return total, nil
}
