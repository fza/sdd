package handlers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
)

// errNoRepos surfaces a repo operation on a handler constructed without the
// connected-repos manager — a wiring gap, never a user state (fail loud).
var errNoRepos = fmt.Errorf("connected-repos support is not wired (handler built without Repos)")

// RepoAdd executes a RepoAddCmd with its two writes: clone the target into
// its sdd-owned clone,
// establish its canonical identity from the repo_id it declares in its
// committed config, register the connection in the user-global config (the
// per-user resolution), and declare the dependency in the current repo's
// committed .sdd/config.yaml (the portable record of what this graph
// needs). When the clone URL itself derives an identity (ssh/https forms),
// the declared value must match it — that cross-check catches pointing at a
// fork or mirror under the wrong name. A URL with no host/path form (a
// local path, an offline mirror) skips the cross-check and trusts the
// declared identity, which is canonical by design. The clone runs before
// registration so a failed verification leaves the config untouched.
// Re-running against an already-connected URL skips the clone and only
// ensures the declaration — the upgrade path for connections made before
// dependencies existed.
func (h *Handler) RepoAdd(ctx context.Context, cmd *command.RepoAddCmd) error {
	if err := cmd.Validate(); err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}
	if h.repos == nil {
		return errNoRepos
	}

	cfg, err := h.repos.Registry().Load()
	if err != nil {
		return err
	}

	// Already connected under this URL: the resolution half is done — only
	// ensure the committed declaration. Errors when that too is already in
	// place, so a true no-op still reads as "nothing to do".
	if existing, ok := cfg.ConnectedByURL(cmd.CloneURL); ok {
		declared, err := h.declareDependency(ctx, existing.RepoID, cmd)
		if err != nil {
			return err
		}
		if !declared {
			return fmt.Errorf("repo %q is already connected and declared", existing.RepoID)
		}
		return nil
	}

	// Past the already-connected no-op: real work follows. Report the stages
	// so the footer shows connecting → cloning rather than a stuck spinner.
	emitPhase(cmd.OnPhase, model.PhaseConnecting)

	// Clone into a staging location first: the canonical identity, and so
	// where the clone lives, is only known once its declared config is
	// readable.
	emitPhase(cmd.OnPhase, model.PhaseCloning)
	staged, removeStaged, err := h.repos.Stage(ctx, cmd.CloneURL)
	if err != nil {
		return err
	}
	defer removeStaged()

	cloneCfg, err := repos.Clone{Dir: staged}.Config()
	if err != nil && !errors.Is(err, meta.ErrNotAnSDDProject) {
		return fmt.Errorf("repo at %s: %w", cmd.CloneURL, err)
	}
	if cloneCfg == nil || cloneCfg.RepoID == "" {
		return fmt.Errorf("repo at %s declares no repo_id in .sdd/config.yaml — run `sdd init` there (or add repo_id) first", cmd.CloneURL)
	}
	declared := cloneCfg.RepoID
	if err := model.ValidateRepoID(declared); err != nil {
		return fmt.Errorf("repo at %s declares invalid repo_id %q: %w", cmd.CloneURL, declared, err)
	}
	if derived, derr := model.DeriveRepoID(cmd.CloneURL); derr == nil && derived != declared {
		return fmt.Errorf("repo at %s declares repo_id %q, but its clone URL derives %q — the declared identity is canonical; check the URL", cmd.CloneURL, declared, derived)
	}
	if _, exists := cfg.Connected(declared); exists {
		return fmt.Errorf("repo %q is already connected", declared)
	}

	repo := repos.ConnectedRepo{RepoID: declared, CloneURL: cmd.CloneURL}
	unlock, err := h.repos.Registry().LockClones(ctx)
	if err != nil {
		return err
	}
	cloneDir, connectErr := h.placeAndConnect(ctx, staged, repo)
	if err := errors.Join(connectErr, unlock()); err != nil {
		return err
	}
	if _, err := h.declareDependency(ctx, declared, cmd); err != nil {
		return err
	}
	if cmd.OnAdded != nil {
		cmd.OnAdded(declared, cloneDir)
	}
	return nil
}

// placeAndConnect places the staged clone and connects the repo, under the
// clone lock the caller holds, so cache pruning never sees the clone in place
// before its repo is connected.
func (h *Handler) placeAndConnect(ctx context.Context, staged string, repo repos.ConnectedRepo) (string, error) {
	cloneDir, err := h.repos.Place(ctx, staged, repo)
	if err != nil {
		return "", err
	}
	return cloneDir, h.connectRepo(repo)
}

// declareDependency ensures repoID is listed in the committed dependencies
// of the current repo's .sdd/config.yaml, using the comment-preserving
// sequence upsert. Reports whether the declaration was added (false: it was
// already there). Outside an sdd repo it is a silent no-op — registering a
// connection without a dependent graph is legitimate. A graph never depends
// on itself.
func (h *Handler) connectRepo(repo repos.ConnectedRepo) error {
	file, err := h.globalConfigFile()
	if err != nil {
		return err
	}
	return file.patch(func(existing []byte) ([]byte, error) {
		cfg, err := repos.ParseGlobalConfig(existing)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.path, err)
		}
		if err := cfg.AddRepo(repo); err != nil {
			return nil, err
		}
		return model.SetYAMLValue(existing, "repos", cfg.Repos)
	})
}

func (h *Handler) declareDependency(ctx context.Context, repoID model.RepoID, cmd *command.RepoAddCmd) (bool, error) {
	if h.sddDir == "" {
		return false, nil
	}
	file, err := h.repoConfigFile()
	if err != nil {
		return false, err
	}
	declared := false
	err = file.patch(func(existing []byte) ([]byte, error) {
		cfgFile, err := model.ParseConfig(existing)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.path, err)
		}
		if cfgFile.RepoID == repoID {
			return nil, fmt.Errorf("%q is this repo's own identity — a graph cannot depend on itself", repoID)
		}
		if slices.Contains(cfgFile.Dependencies, repoID) {
			return nil, nil
		}
		patched, err := model.SetYAMLSequence(existing, "dependencies", append(cfgFile.Dependencies, repoID))
		if err != nil {
			return nil, fmt.Errorf("declaring dependency in %s: %w", file.path, err)
		}
		declared = true
		return patched, nil
	})
	if err != nil {
		return false, err
	}
	if !declared {
		if cmd.OnDeclared != nil {
			cmd.OnDeclared(repoID, true)
		}
		return false, nil
	}
	// Auto-commit the declaration, consistent with sdd init/new/summarize —
	// the committed dependencies list is a shared, go.mod-style record other
	// clones read to know what to connect, so it must not linger uncommitted.
	if h.committer != nil {
		if err := h.commitGraph(ctx, fmt.Sprintf("sdd: repo add %s", repoID), file.path); err != nil {
			return false, fmt.Errorf("committing dependency declaration: %w", err)
		}
	}
	if cmd.OnDeclared != nil {
		cmd.OnDeclared(repoID, false)
	}
	return true, nil
}

// RepoRemove executes a RepoRemoveCmd: it drops repoID from the committed
// dependencies of the current repo's .sdd/config.yaml and commits that
// change. Project-scoped and reference-safety-guarded — it refuses when any
// local entry still holds a cross-repo ref into the target (dropping the
// declaration would strand those refs and retroactively break
// resolve-or-block), unless Force is set, in which case the stranded refs are
// named through OnStranded before the removal proceeds. The per-user global
// connection and its clone are left untouched — machine-level teardown is a
// separate command surface, out of scope here.
func (h *Handler) RepoRemove(ctx context.Context, cmd *command.RepoRemoveCmd) error {
	if err := cmd.Validate(); err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}
	if h.sddDir == "" {
		return fmt.Errorf("`sdd repo remove` is project-scoped — run it inside an sdd repo to drop a declared dependency")
	}
	slogutils.FromContext(ctx).Debug("removing declared dependency", "repo", cmd.RepoID, "force", cmd.Force)

	file, err := h.repoConfigFile()
	if err != nil {
		return err
	}
	data, err := file.read()
	if err != nil {
		return err
	}
	if data == nil {
		return fmt.Errorf("no .sdd/config.yaml here — nothing to remove")
	}
	cfgFile, err := model.ParseConfig(data)
	if err != nil {
		return fmt.Errorf("%s: %w", file.path, err)
	}
	if !slices.Contains(cfgFile.Dependencies, cmd.RepoID) {
		return fmt.Errorf("repo %q is not a declared dependency in .sdd/config.yaml", cmd.RepoID)
	}

	// Ref-safety guard: a local entry referencing the target would be
	// orphaned by dropping the declaration. Refuse unless forced, and name
	// the refs either way so the block (or the override) is never silent.
	stranded, err := h.strandedRefs(cmd.RepoID)
	if err != nil {
		return err
	}
	if len(stranded) > 0 {
		if !cmd.Force {
			return fmt.Errorf("refusing to remove %q: %d local %s still reference it — removal would strand:\n%s\nre-run with --force to remove anyway",
				cmd.RepoID, len(stranded), pluralEntries(len(stranded)), formatStranded(stranded))
		}
		if cmd.OnStranded != nil {
			cmd.OnStranded(cmd.RepoID, stranded)
		}
	}

	remaining := slices.DeleteFunc(slices.Clone(cfgFile.Dependencies), func(d model.RepoID) bool { return d == cmd.RepoID })
	if err := file.patch(func(existing []byte) ([]byte, error) {
		patched, err := model.SetYAMLSequence(existing, "dependencies", remaining)
		if err != nil {
			return nil, fmt.Errorf("removing dependency from %s: %w", file.path, err)
		}
		return patched, nil
	}); err != nil {
		return err
	}
	if h.committer != nil {
		if err := h.commitGraph(ctx, fmt.Sprintf("sdd: repo remove %s", cmd.RepoID), file.path); err != nil {
			return fmt.Errorf("committing dependency removal: %w", err)
		}
	}
	if cmd.OnRemoved != nil {
		cmd.OnRemoved(cmd.RepoID)
	}
	return nil
}

// strandedRefs scans the current graph for local entries that hold a
// cross-repo reference into repoID — the refs a `repo remove` of that
// dependency would orphan. Cross-repo lifecycle effects (closes, supersedes)
// are within-graph only, so only Refs can point across the boundary.
func (h *Handler) strandedRefs(repoID model.RepoID) ([]command.StrandedRef, error) {
	if h.reader == nil || h.graphDir == "" {
		return nil, nil
	}
	graph, err := h.reader.CurrentGraph(h.graphDir)
	if err != nil {
		return nil, fmt.Errorf("loading graph for ref-safety check: %w", err)
	}
	var out []command.StrandedRef
	for _, e := range graph.Entries {
		for _, r := range e.Refs {
			if target, _, ok := model.SplitCrossRepoID(r.ID); ok && target == repoID {
				out = append(out, command.StrandedRef{EntryID: e.ID, RefID: r.ID, Kind: string(r.Kind)})
			}
		}
	}
	return out, nil
}

// formatStranded renders stranded refs as an indented, aligned block for the
// refusal message and the forced-removal warning.
func formatStranded(stranded []command.StrandedRef) string {
	var b strings.Builder
	for _, s := range stranded {
		fmt.Fprintf(&b, "  %s  %s  %s\n", s.EntryID, s.Kind, s.RefID)
	}
	return strings.TrimRight(b.String(), "\n")
}

func pluralEntries(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}

// PrepareCrossRepoSearch runs the side-effect half of a cross-graph search,
// mirroring how the local lazy-fill precedes the search finder: refresh the
// dependency closure, select the query's repos from it, and — when vector
// mode is in play — lazy-fill each selected repo's index with the shared
// embedder. The finder read (finders.MultiSearch) then runs pure. fill is
// optional: the CLI passes progress callbacks so the member builds render
// through the output coordinator.
func (h *Handler) PrepareCrossRepoSearch(ctx context.Context, q query.SearchQuery, embedder IndexEmbedder, refresh command.RefreshDependencyClosureCmd, fill *command.BuildConnectedIndexesCmd) error {
	if !q.AllRepos && len(q.Repos) == 0 {
		return nil
	}
	closure, err := h.refreshDependencyClosure(ctx, refresh)
	if err != nil {
		return err
	}
	repoIDs, err := model.SelectRepos(q.Repos, q.AllRepos, closure)
	if err != nil || q.Phrase == "" {
		return err
	}
	return h.BuildConnectedIndexes(ctx, repoIDs, embedder, fill)
}

// RefreshDependencyClosure walks the local repo's dependency closure,
// refreshing each connected repo's clone (made on first use, else its sync
// round when due) before reading what it declares, so a config repaired
// upstream is read after its round. What the refresh cannot do is reported through the command's
// callbacks; only a failure to read the closure itself is returned.
func (h *Handler) RefreshDependencyClosure(ctx context.Context, cmd command.RefreshDependencyClosureCmd) error {
	_, err := h.refreshDependencyClosure(ctx, cmd)
	return err
}

func (h *Handler) refreshDependencyClosure(ctx context.Context, cmd command.RefreshDependencyClosureCmd) ([]model.RepoID, error) {
	if err := cmd.Validate(); err != nil {
		return nil, err
	}
	if h.repos == nil {
		return nil, errNoRepos
	}
	root, err := h.reader.LocalRepoID()
	if err != nil {
		return nil, err
	}
	step := func(ctx context.Context, repoID model.RepoID) ([]model.RepoID, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if repoID != root {
			if err := h.refreshClone(ctx, repoID, cmd); err != nil {
				return nil, err
			}
		}
		return h.reader.DeclaredDependencies(ctx, repoID)
	}
	var closure []model.RepoID
	for repoID, err := range model.DependencyClosure(ctx, root, model.DependencyResolverFunc[model.RepoID](step)) {
		if err != nil {
			return nil, err
		}
		closure = append(closure, repoID)
	}
	return closure, nil
}

// refreshClone refreshes one closure member, reporting a missing connection
// or a failed clone or sync round instead of returning it.
func (h *Handler) refreshClone(ctx context.Context, repoID model.RepoID, cmd command.RefreshDependencyClosureCmd) error {
	_, connected, err := h.repos.Registry().Clone(repoID)
	if err != nil {
		return err
	}
	if !connected {
		cmd.OnUnconnected(repoID)
		return nil
	}
	if _, err := h.EnsureReposFresh(ctx, command.EnsureReposFreshCmd{RepoIDs: []model.RepoID{repoID}, OnPhase: cmd.OnPhase}); err != nil {
		cmd.OnRefreshFailed(repoID, err)
	}
	return nil
}

// BuildConnectedIndexes fills each given repo's index under the shared
// embedder, excluding embedded entries; callers refresh the clones first.
// Entries indexed under a different fingerprint re-embed here rather than
// excluding the repo, so one vector space holds across every selected index.
// It is the shared spine for eager pre-indexing (`sdd index --repo`) and the
// fill half of a cross-repo search's prepare step. fill's callbacks report
// progress per repo and aggregate across them; a nil fill runs quiet. When
// fill.Force is set (only `sdd index --repo/--all-repos --force`), each member
// store is fully rebuilt rather than lazily reconciled, repairing stale or
// corrupt connected indexes.
func (h *Handler) BuildConnectedIndexes(ctx context.Context, repoIDs []model.RepoID, embedder IndexEmbedder, fill *command.BuildConnectedIndexesCmd) error {
	if h.repos == nil {
		return errNoRepos
	}
	if len(repoIDs) == 0 || embedder.Embedder == nil {
		return nil
	}
	if fill == nil {
		fill = &command.BuildConnectedIndexesCmd{}
	}
	for _, repoID := range repoIDs {
		cloneDir, err := h.repos.Registry().CloneDir(repoID)
		if err != nil {
			return err
		}
		clone := repos.Clone{RepoID: repoID, Dir: cloneDir}
		if !clone.Cloned() {
			continue // unconnected or clone failed — the finder reports the skip
		}
		graphDir, err := clone.GraphDir()
		if err != nil {
			return err
		}
		// One machine-global store per (repo-id, fingerprint): the same
		// index the repo's own checkout would use, embedded once. The handler
		// loads and locks the store itself at write time (index.FillSession).
		idxDir := index.StoreDir(h.repos.Registry().CacheRoot(), index.RepoKey(repoID, cloneDir), embedder.Fingerprint())
		ih := NewIndexHandler(IndexHandlerOptions{
			GraphDir:        graphDir,
			IndexDir:        idxDir,
			Embedder:        embedder,
			Reader:          h.reader,
			ExcludeEmbedded: true,
		})
		if fill.OnRepoStart != nil {
			fill.OnRepoStart(repoID)
		}
		// Force rebuilds the whole member store (repairing a stale or corrupt
		// index); the default lazy reconcile only touches what changed.
		if fill.Force {
			if err := ih.Build(ctx, &command.BuildIndexCmd{
				Force:          true,
				OnPlanned:      fill.OnPlanned,
				OnBatchStart:   fill.OnBatchStart,
				OnEntryIndexed: fill.OnEntryIndexed,
			}); err != nil {
				return fmt.Errorf("indexing %s: %w", repoID, err)
			}
			continue
		}
		if err := ih.LazyFill(ctx, &command.LazyFillIndexCmd{
			OnPlanned:      fill.OnPlanned,
			OnBatchStart:   fill.OnBatchStart,
			OnEntryIndexed: fill.OnEntryIndexed,
		}); err != nil {
			return fmt.Errorf("indexing %s: %w", repoID, err)
		}
	}
	return nil
}

// emitPhase fires an optional phase callback, guarding the nil case at the one
// point phases cross the CQRS boundary (handlers report phases as plain
// callback data; internal/cliout stays out of these layers).
func emitPhase(onPhase func(model.Phase), phase model.Phase) {
	if onPhase != nil {
		onPhase(phase)
	}
}
