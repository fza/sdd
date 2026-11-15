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
// repos are connected and where their caches live. It is the dependency the
// read side (finders) gets injected — it carries no way to clone, pull, or
// save, so finder purity is enforced by the type, not by discipline. The
// side-effectful lifecycle lives on Manager.
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

// CacheRoot is the base directory for connected-repo clone caches.
func (r *Registry) CacheRoot() string {
	return r.loc.CacheRoot
}

// CacheDir resolves a connected repo's clone location under the cache root.
// The repo-id's host/path shape nests naturally under the root.
func (r *Registry) CacheDir(repoID model.RepoID) (string, error) {
	if err := model.ValidateRepoID(repoID); err != nil {
		return "", err
	}
	return filepath.Join(r.loc.CacheRoot, filepath.FromSlash(string(repoID))), nil
}

// Cache returns a connected repo's cache; ok is false when the repo is not
// connected on this machine.
func (r *Registry) Cache(repoID model.RepoID) (c Cache, ok bool, err error) {
	cfg, err := r.Load()
	if err != nil {
		return Cache{}, false, err
	}
	if _, connected := cfg.Connected(repoID); !connected {
		return Cache{}, false, nil
	}
	dir, err := r.CacheDir(repoID)
	if err != nil {
		return Cache{}, false, err
	}
	return Cache{RepoID: repoID, Dir: dir}, true, nil
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

// ErrClonePassRefused reports a clone pass that cannot trust the repos list it
// would judge clones against; the wrapping error says why.
var ErrClonePassRefused = errors.New("refusing to remove clones")

// UnconnectedClones lists the clones a cache pruning run may remove: the
// clones of repositories `sdd repo remove` dropped (see Disconnected) that the
// user-global repos list no longer names, and the clones an earlier run
// retired. A repo missing from the list proves nothing about a clone, as a
// process may read another machine's or another user's config root, so a
// clone without a disconnection record stays. A clone is a directory holding
// .git below a top-level directory named like a host: repo IDs open with a
// dotted hostname, which keeps the index and graph caches beside them out of
// the walk. It returns ErrClonePassRefused, listing nothing, when the
// locations come from mixed XDG sources, or the config file is missing or
// lists no repos, while a clone would go.
func (r *Registry) UnconnectedClones() ([]string, error) {
	cfg, err := r.Load()
	if err != nil {
		return nil, err
	}
	hosts, err := os.ReadDir(r.loc.CacheRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing clone caches: %w", err)
	}
	var stale []string
	judged := 0
	for _, host := range hosts {
		if !host.IsDir() || strings.HasPrefix(host.Name(), ".") || !strings.Contains(host.Name(), ".") {
			continue
		}
		root := filepath.Join(r.loc.CacheRoot, host.Name())
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			if _, statErr := os.Stat(filepath.Join(path, ".git")); statErr != nil {
				return nil
			}
			if isRetiredClone(path) {
				stale = append(stale, path)
				return filepath.SkipDir
			}
			repoID, err := r.cloneRepoID(path)
			if err != nil {
				return err
			}
			if _, connected := cfg.Connected(repoID); connected {
				return filepath.SkipDir
			}
			dropped, err := r.Disconnected(repoID)
			if err != nil {
				return err
			}
			if dropped {
				stale = append(stale, path)
				judged++
			}
			return filepath.SkipDir
		})
		if err != nil {
			return nil, fmt.Errorf("listing clone caches under %s: %w", root, err)
		}
	}
	if judged > 0 {
		if err := r.trustRepoList(cfg, judged); err != nil {
			return nil, err
		}
	}
	return stale, nil
}

// trustRepoList refuses a repos list that may not be this machine's while
// judged clones would go.
func (r *Registry) trustRepoList(cfg *GlobalConfig, judged int) error {
	if r.loc.MixedXDG != "" {
		return fmt.Errorf("%w: the XDG base directories come from mixed sources (%s) while %d clones under %s would go; set all of them or none", ErrClonePassRefused, r.loc.MixedXDG, judged, r.loc.CacheRoot)
	}
	_, err := os.Stat(r.loc.ConfigPath)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s does not exist while %d clones under %s would go; check XDG_CONFIG_HOME", ErrClonePassRefused, r.loc.ConfigPath, judged, r.loc.CacheRoot)
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", r.loc.ConfigPath, err)
	}
	if len(cfg.Repos) == 0 {
		return fmt.Errorf("%w: %s lists no repos while %d clones under %s would go; delete them by hand if they should go", ErrClonePassRefused, r.loc.ConfigPath, judged, r.loc.CacheRoot)
	}
	return nil
}

func (r *Registry) cloneRepoID(path string) (model.RepoID, error) {
	rel, err := filepath.Rel(r.loc.CacheRoot, path)
	if err != nil {
		return "", err
	}
	return model.RepoID(filepath.ToSlash(rel)), nil
}

func isRetiredClone(dir string) bool {
	base := filepath.Base(dir)
	return strings.HasPrefix(base, ".") && strings.HasSuffix(base, ".pruned")
}
