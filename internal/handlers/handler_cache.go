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
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/llm/embed"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/roundproc"
)

// CurrentHashSource supplies the current state hash of each entry in the
// checkout a pruning run starts from, whose current versions it never deletes.
type CurrentHashSource interface {
	CurrentHashes(ctx context.Context, maxRunes int) (map[string]string, error)
}

// OwnedCloneProbe answers what pruning asks of an sdd-owned clone before it
// removes one.
type OwnedCloneProbe interface {
	// TryLockSync takes the clone's sync lock; release is nil while another
	// process holds it.
	TryLockSync(ctx context.Context, dir string) (release func(), err error)
	// Unpushed reports whether the clone holds commits its remote lacks.
	Unpushed(ctx context.Context, dir string) (bool, error)
	// LastAttempt is when the git gate last admitted a fetch or push of the
	// clone's remote; zero when it never did.
	LastAttempt(ctx context.Context, dir string) (time.Time, error)
	// GC runs git gc --auto in the clone, locally.
	GC(ctx context.Context, dir string) error
}

// GateSweeper is the git gate's part in the sweep.
type GateSweeper interface {
	PruneRecords(ctx context.Context, now time.Time, retention time.Duration) (int, error)
	SweepTraces(now time.Time, minAge time.Duration) (int, error)
}

// StateSweeper tends the per-project state under the state root: sessions
// past sessionRetention and projects unused for unusedRetention.
type StateSweeper interface {
	Sweep(ctx context.Context, now time.Time, sessionRetention, unusedRetention time.Duration) (model.CachePruneCounts, error)
}

// CacheHandlerOptions configures NewCacheHandler. CacheRoot, StateRoot,
// Registry, OwnedClones, Gate and State are required; Keep is nil outside an
// sdd checkout; Now defaults to time.Now.
type CacheHandlerOptions struct {
	CacheRoot   string
	StateRoot   string
	Registry    *repos.Registry
	OwnedClones OwnedCloneProbe
	Gate        GateSweeper
	State       StateSweeper
	Keep        CurrentHashSource
	// DeclaredHooks returns the hashes of the hooks a checkout declares now
	// (hooks.DeclaredIn); the pass drops every other hook confirmation under
	// StateRoot. Nil leaves the confirmations alone.
	DeclaredHooks func(checkout string) ([]string, error)
	// SecretRules fetches gitleaks' newest rule file in a scheduled pass
	// while secrets.rules_update is daily; nil fetches nothing.
	SecretRules SecretRulesUpdater
	Now         func() time.Time
}

// SecretRulesUpdater fetches a newer credential rule file when there is one.
type SecretRulesUpdater interface {
	Update(ctx context.Context) error
}

// CacheHandler runs the machine-global cache pruning.
type CacheHandler struct {
	cacheRoot     string
	stateRoot     string
	registry      *repos.Registry
	ownedClones   OwnedCloneProbe
	gate          GateSweeper
	state         StateSweeper
	keep          CurrentHashSource
	declaredHooks func(checkout string) ([]string, error)
	secretRules   SecretRulesUpdater
	now           func() time.Time
}

func NewCacheHandler(opts CacheHandlerOptions) *CacheHandler {
	h := &CacheHandler{cacheRoot: opts.CacheRoot, stateRoot: opts.StateRoot, registry: opts.Registry, ownedClones: opts.OwnedClones, gate: opts.Gate, state: opts.State, keep: opts.Keep, declaredHooks: opts.DeclaredHooks, secretRules: opts.SecretRules, now: opts.Now}
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
	counts, runErr := h.prune(runCtx, now, cmd.Scheduled)
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

func (h *CacheHandler) prune(ctx context.Context, now time.Time, scheduled bool) (model.CachePruneCounts, error) {
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
	sessionRetention, err := model.ResolveSessionRetention(cfg.Sessions)
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

	if h.declaredHooks != nil {
		if err := hooks.NewStore(h.stateRoot).Sweep(h.declaredHooks); err != nil {
			errs = append(errs, fmt.Errorf("sweeping hook confirmations: %w", err))
		}
	}

	errs = append(errs, h.sweepLeftovers(now, &counts)...)
	errs = append(errs, h.pruneRecords(ctx, now, retention, &counts)...)

	if h.state == nil {
		errs = append(errs, errors.New("cache pruning has no sweeper for the state folder"))
	} else {
		swept, err := h.state.Sweep(ctx, now, sessionRetention, retention)
		counts.Add(swept)
		if err != nil {
			errs = append(errs, fmt.Errorf("sweeping the state folder: %w", err))
		}
	}

	if scheduled && h.secretRules != nil && model.ResolveSecretRulesUpdate(cfg.Secrets) == model.SecretRulesDaily {
		// The updater records a failure where sdd info shows it; scans keep
		// the copy in use, so the pass goes on.
		if err := h.secretRules.Update(ctx); err != nil {
			log.Warn("fetching gitleaks' newest rule file", "err", err)
		}
	}

	logs, freed, err := roundproc.PruneLogs(filepath.Join(h.stateRoot, roundproc.LogDirName), now, retention)
	counts.Bytes += freed
	if err != nil {
		errs = append(errs, fmt.Errorf("pruning round logs: %w", err))
	}
	if logs > 0 {
		log.Info("removed round logs", "logs", logs, "bytes", freed)
	}

	clones, err := h.registry.UnconnectedClones()
	if err != nil {
		errs = append(errs, err)
	}
	for _, clone := range clones {
		dir := clone.Dir
		retired, err := h.registry.RetireUnconnectedClone(clone, h.holdUnused(ctx, now, retention))
		switch {
		case errors.Is(err, repos.ErrClonesBusy):
			log.Info("connected-repo clones in use, skipped")
			continue
		case errors.Is(err, repos.ErrCloneInUse):
			log.Info("clone kept", "clone", dir, "reason", err)
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
	errs = append(errs, h.gcListedClones(ctx)...)

	errs = append(errs, h.pruneIndex(ctx, now, index.Retention{Unused: retention, Replaced: replaced}, keepFor, &counts)...)
	return counts, errors.Join(errs...)
}

// gcListedClones runs git gc --auto in each clone the repos list names,
// skipping one a sync holds.
func (h *CacheHandler) gcListedClones(ctx context.Context) []error {
	if h.ownedClones == nil {
		return []error{errors.New("cache pruning has no probe for sdd-owned clones")}
	}
	clones, err := h.registry.ListedClones()
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, clone := range clones {
		if ctx.Err() != nil {
			break
		}
		release, err := h.ownedClones.TryLockSync(ctx, clone.Dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("locking the sync of %s: %w", clone.Dir, err))
			continue
		}
		if release == nil {
			slogutils.FromContext(ctx).Info("clone in use, gc skipped", "clone", clone.Dir)
			continue
		}
		err = h.ownedClones.GC(ctx, clone.Dir)
		release()
		if err != nil {
			errs = append(errs, fmt.Errorf("packing the clone of %s: %w", clone.RepoID, err))
		}
	}
	return errs
}

// sweepLeftovers removes what killed processes left outside the index stores
// once older than model.CacheLeftoverAge.
func (h *CacheHandler) sweepLeftovers(now time.Time, counts *model.CachePruneCounts) []error {
	var errs []error
	if h.gate == nil {
		errs = append(errs, errors.New("cache pruning has no git gate"))
	} else {
		traces, err := h.gate.SweepTraces(now, model.CacheLeftoverAge)
		counts.Leftovers += traces
		if err != nil {
			errs = append(errs, err)
		}
	}
	staging, err := h.registry.SweepCloneStaging(now, model.CacheLeftoverAge)
	counts.Leftovers += staging
	if err != nil {
		errs = append(errs, err)
	}
	hosts, err := h.registry.SweepLegacyHostDirs(now, model.CacheLeftoverAge)
	counts.Leftovers += hosts
	if err != nil {
		errs = append(errs, err)
	}
	return errs
}

// pruneRecords drops git gate records and embedder calibrations unused for
// retention.
func (h *CacheHandler) pruneRecords(ctx context.Context, now time.Time, retention time.Duration, counts *model.CachePruneCounts) []error {
	var errs []error
	if h.gate != nil {
		records, err := h.gate.PruneRecords(ctx, now, retention)
		counts.Records += records
		if err != nil {
			errs = append(errs, err)
		}
	}
	calibrations, err := embed.PruneCalibrations(filepath.Join(h.cacheRoot, embed.CalibrationDirName), now, retention)
	counts.Records += calibrations
	if err != nil {
		errs = append(errs, err)
	}
	return errs
}

// pruneIndex applies the index rules of 20261114-095140-d-cpt-ad4 to every
// store, the longest pass, which the run limit cuts short first.
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

// holdUnused lets an sdd-owned clone go only while nothing uses it: its sync
// lock is free, it holds no unpushed commits, and the git gate admitted no
// operation on its remote within retention, which keeps the graph_repo
// clone of a project, never connected, while the project syncs it.
func (h *CacheHandler) holdUnused(ctx context.Context, now time.Time, retention time.Duration) repos.CloneHold {
	return func(dir string) (func(), error) {
		if h.ownedClones == nil {
			return nil, errors.New("cache pruning has no probe for sdd-owned clones")
		}
		release, err := h.ownedClones.TryLockSync(ctx, dir)
		if err != nil {
			return nil, fmt.Errorf("locking the sync of %s: %w", dir, err)
		}
		if release == nil {
			return nil, fmt.Errorf("%w: a sync holds it", repos.ErrCloneInUse)
		}
		if err := h.cloneInUse(ctx, dir, now, retention); err != nil {
			release()
			return nil, err
		}
		return release, nil
	}
}

func (h *CacheHandler) cloneInUse(ctx context.Context, dir string, now time.Time, retention time.Duration) error {
	unpushed, err := h.ownedClones.Unpushed(ctx, dir)
	if err != nil {
		return err
	}
	if unpushed {
		return fmt.Errorf("%w: it holds unpushed commits", repos.ErrCloneInUse)
	}
	last, err := h.ownedClones.LastAttempt(ctx, dir)
	if err != nil {
		return err
	}
	if !last.IsZero() && now.Sub(last) <= retention {
		return fmt.Errorf("%w: git last reached its remote at %s", repos.ErrCloneInUse, last.Format(time.RFC3339))
	}
	return nil
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
