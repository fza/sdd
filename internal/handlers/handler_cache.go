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

// ErrCachePruneRunning refuses a pass asked for by hand while another
// process runs one; a scheduled pass leaves quietly instead.
var ErrCachePruneRunning = errors.New("another cache pruning run is in progress")

// Prune runs one pruning pass over the whole cache and records its result in
// the pruning status, failure included, so the next command can report it.
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
		if cmd.Scheduled {
			slogutils.FromContext(ctx).Info("cache pruning already running in another process")
			return nil
		}
		return ErrCachePruneRunning
	}
	defer func() { err = errors.Join(err, run.Unlock()) }()

	now := h.now()
	if cmd.Scheduled {
		status, err := cacheprune.Load(h.stateRoot)
		if err != nil {
			return err
		}
		if now.Sub(status.StartedAt) < model.CachePruneInterval {
			return nil
		}
	}
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
		s.Error, s.Reported = "", false
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

	stores, leftovers, err := index.Stores(h.cacheRoot)
	if err != nil {
		errs = append(errs, err)
	}
	for _, dir := range leftovers {
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", dir, err))
		}
	}
	for _, dir := range stores {
		if ctx.Err() != nil {
			log.Info("cache pruning stopped at its time limit; the next run continues")
			break
		}
		last, err := index.StoreLastUse(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if now.Sub(last) > retention {
			freed, err := index.RemoveStore(dir, now, retention)
			switch {
			case errors.Is(err, index.ErrLockBusy):
				log.Info("index store in use, skipped", "store", dir)
			case err != nil:
				errs = append(errs, err)
			case freed > 0:
				counts.Stores++
				counts.Bytes += freed
				log.Info("removed unused index store", "store", dir, "bytes", freed)
			}
			continue
		}
		keep, err := keepFor(dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("pruning %s: %w", dir, err))
			continue
		}
		pruned, err := index.PruneStore(ctx, dir, now, retention, keep)
		counts.Versions += pruned.Versions
		counts.Bytes += pruned.Bytes
		switch {
		case errors.Is(err, index.ErrLockBusy):
			log.Info("index store in use, skipped", "store", dir)
		case err != nil && ctx.Err() != nil:
			log.Info("cache pruning stopped at its time limit; the next run continues", "store", dir)
		case err != nil:
			errs = append(errs, fmt.Errorf("pruning %s: %w", dir, err))
		case pruned.Versions > 0:
			log.Info("pruned index versions", "store", dir, "versions", pruned.Versions, "bytes", pruned.Bytes)
		}
	}
	return counts, errors.Join(errs...)
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
