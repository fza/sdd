package repos

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// Registry is the pure read surface over the connected-repos state: which
// repos are connected and where their sdd-owned clones live. It is the
// dependency the read side (finders) gets injected — it carries no way to
// clone, sync, or save, so finder purity is enforced by the type, not by
// discipline. The side-effectful lifecycle lives on Manager.
type Registry struct {
	loc Locations
}

// NewRegistry builds a Registry over explicit locations. Composition roots
// pass DefaultLocations(); tests pass temp dirs.
func NewRegistry(loc Locations) *Registry {
	return &Registry{loc: loc}
}

// Load reads the user-global config. Deliberately lazy — read per call, not
// snapshotted at construction — so a long-lived process (the MCP server)
// sees a repo connected from another terminal without a restart. A missing
// file yields an empty config.
func (r *Registry) Load() (*GlobalConfig, error) {
	return LoadConfigFrom(r.loc.ConfigPath)
}

// ConfigPath returns the user-global config file path. Exposed for the
// comment-preserving `sdd config set` upsert, which patches the file bytes
// in place rather than round-tripping through Save (which would drop
// comments).
func (r *Registry) ConfigPath() string {
	return r.loc.ConfigPath
}

// CacheRoot is the base directory of the machine-global search index stores.
func (r *Registry) CacheRoot() string {
	return r.loc.CacheRoot
}

// MixedStateXDG is Locations.MixedStateXDG of the locations the registry
// works against.
func (r *Registry) MixedStateXDG() string {
	return r.loc.MixedStateXDG
}

// CloneDir resolves where the sdd-owned clone of repoID lives, one per
// repository id under <DataRoot>/repos.
func (r *Registry) CloneDir(repoID model.RepoID) (string, error) {
	return cloneDir(r.loc.DataRoot, repoID)
}

// LegacyCacheDir is where an sdd that keeps read-only clones under the cache
// root places the one of repoID, for migrating the index it may still hold.
// The clone itself is that sdd's and is never removed.
func (r *Registry) LegacyCacheDir(repoID model.RepoID) (string, error) {
	return confinedClonePath(r.loc.CacheRoot, repoID)
}

// Clone returns a connected repo's clone; ok is false when the repo is not
// connected on this machine.
func (r *Registry) Clone(repoID model.RepoID) (c Clone, ok bool, err error) {
	cfg, err := r.Load()
	if err != nil {
		return Clone{}, false, err
	}
	if _, connected := cfg.Connected(repoID); !connected {
		return Clone{}, false, nil
	}
	dir, err := r.CloneDir(repoID)
	if err != nil {
		return Clone{}, false, err
	}
	return Clone{RepoID: repoID, Dir: dir}, true, nil
}

// SelectRepoIDs resolves a caller's repo selection against the connected
// set (see GlobalConfig.SelectRepoIDs).
func (r *Registry) SelectRepoIDs(named []model.RepoID, all bool) ([]model.RepoID, error) {
	if !all && len(named) == 0 {
		return nil, nil
	}
	cfg, err := r.Load()
	if err != nil {
		return nil, err
	}
	return cfg.SelectRepoIDs(named, all)
}

// ListedClones returns the sdd-owned clones present on this machine whose
// repository the user-global repos list names.
func (r *Registry) ListedClones() ([]Clone, error) {
	if r.loc.DataRoot == "" {
		return nil, errors.New("listing clones: no data root for the sdd-owned clones")
	}
	cfg, err := r.Load()
	if err != nil {
		return nil, err
	}
	var clones []Clone
	for _, repo := range cfg.Repos {
		dir, err := cloneDir(r.loc.DataRoot, repo.RepoID)
		if err != nil {
			return nil, err
		}
		if IsCloned(dir) {
			clones = append(clones, Clone{RepoID: repo.RepoID, Dir: dir})
		}
	}
	return clones, nil
}

// ErrClonePassRefused reports a clone pass that cannot trust the repos list
// it would judge clones against; the wrapping error says why.
var ErrClonePassRefused = errors.New("refusing to remove clones")

// UnconnectedClone is an sdd-owned clone whose repository the user-global
// repos list does not name.
type UnconnectedClone struct {
	Dir string
	// RepoID is empty for a clone an earlier run retired.
	RepoID model.RepoID
}

// UnconnectedClones lists the clones a cache pruning run may remove: the
// sdd-owned clones of repositories `sdd repo remove` dropped (see
// Disconnected) that the user-global repos list no longer names, and the
// clones an earlier run retired but did not delete. A repo missing from the
// list proves nothing about a clone, so a clone without a disconnection
// record stays. A clone is a directory holding .git below a top-level
// directory named like a host: repo IDs open with a dotted hostname. Clones
// under the cache root are never listed. It returns ErrClonePassRefused,
// listing nothing, when the locations come from mixed XDG sources or the
// config file is missing or lists no repos while a recorded clone would go.
func (r *Registry) UnconnectedClones() ([]UnconnectedClone, error) {
	if r.loc.DataRoot == "" {
		return nil, errors.New("listing clones: no data root for the sdd-owned clones")
	}
	cfg, err := r.Load()
	if err != nil {
		return nil, err
	}
	clones, err := r.retiredClones()
	if err != nil {
		return nil, err
	}
	found, err := unconnectedClonesUnder(r.clonesBase(), cfg)
	if err != nil {
		return nil, err
	}
	judged := 0
	for _, clone := range found {
		if model.ValidateRepoID(clone.RepoID) != nil {
			continue
		}
		dropped, err := r.Disconnected(clone.RepoID)
		if err != nil {
			return nil, err
		}
		if dropped {
			clones = append(clones, clone)
			judged++
		}
	}
	if judged > 0 {
		err = r.trustRepoList(cfg, judged)
		if err != nil {
			return nil, err
		}
	}
	return clones, nil
}

// trustRepoList refuses a repos list that may not be this machine's while
// judged clones would go.
func (r *Registry) trustRepoList(cfg *GlobalConfig, judged int) error {
	if r.loc.MixedXDG != "" {
		return fmt.Errorf("%w: the XDG base directories come from mixed sources (%s) while %d clones under %s would go; set all four or none", ErrClonePassRefused, r.loc.MixedXDG, judged, r.loc.DataRoot)
	}
	_, err := os.Stat(r.loc.ConfigPath)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s does not exist while %d clones under %s would go; check XDG_CONFIG_HOME", ErrClonePassRefused, r.loc.ConfigPath, judged, r.loc.DataRoot)
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", r.loc.ConfigPath, err)
	}
	if len(cfg.Repos) == 0 {
		return fmt.Errorf("%w: %s lists no repos while %d clones under %s would go; delete them by hand if they should go", ErrClonePassRefused, r.loc.ConfigPath, judged, r.loc.DataRoot)
	}
	return nil
}

// clonesBase is where the sdd-owned clones live.
func (r *Registry) clonesBase() string {
	return filepath.Join(r.loc.DataRoot, "repos")
}

// retiredClonesDirName holds the clones RetireUnconnectedClone moved out of
// their place until they are deleted, beside them on the same file system.
// '~' is no repo ID character, so no clone can live under it.
const retiredClonesDirName = ".clones~retired"

func (r *Registry) retiredClonesDir() string {
	return filepath.Join(r.clonesBase(), retiredClonesDirName)
}

func (r *Registry) isRetiredClone(clone UnconnectedClone) bool {
	return filepath.Dir(clone.Dir) == r.retiredClonesDir()
}

func (r *Registry) retiredClones() ([]UnconnectedClone, error) {
	dir := r.retiredClonesDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing retired clones: %w", err)
	}
	var retired []UnconnectedClone
	for _, entry := range entries {
		if entry.IsDir() {
			retired = append(retired, UnconnectedClone{Dir: filepath.Join(dir, entry.Name())})
		}
	}
	return retired, nil
}

func unconnectedClonesUnder(base string, cfg *GlobalConfig) ([]UnconnectedClone, error) {
	hosts, err := os.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing clones under %s: %w", base, err)
	}
	var stale []UnconnectedClone
	for _, host := range hosts {
		if !host.IsDir() || strings.HasPrefix(host.Name(), ".") || !strings.Contains(host.Name(), ".") {
			continue
		}
		root := filepath.Join(base, host.Name())
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			if strings.HasSuffix(d.Name(), model.GraphCloneStagingSuffix) {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(path, ".git")); statErr != nil {
				return nil
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			repoID := model.RepoID(filepath.ToSlash(rel))
			if _, connected := cfg.Connected(repoID); !connected {
				stale = append(stale, UnconnectedClone{Dir: path, RepoID: repoID})
			}
			return filepath.SkipDir
		})
		if err != nil {
			return nil, fmt.Errorf("listing clones under %s: %w", root, err)
		}
	}
	return stale, nil
}

func cloneDir(dataRoot string, repoID model.RepoID) (string, error) {
	return confinedClonePath(filepath.Join(dataRoot, "repos"), repoID)
}

// confinedClonePath joins repoID below root. Clones are placed, cleared and
// written through that path, so one at or above root, or reaching through a
// symlink below it, is refused.
func confinedClonePath(root string, repoID model.RepoID) (string, error) {
	err := model.ValidateRepoID(repoID)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, filepath.FromSlash(string(repoID)))
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("repo ID %q resolves outside %s", repoID, root)
	}
	component := root
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		component = filepath.Join(component, seg)
		info, err := os.Lstat(component)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("checking the clone path of %s: %w", repoID, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("repo ID %q resolves through the symlink %s", repoID, component)
		}
	}
	return dir, nil
}
