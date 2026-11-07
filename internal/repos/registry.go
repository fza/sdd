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

// CloneDir resolves where the sdd-owned clone of repoID lives, one per
// repository id under <DataRoot>/repos (20261107-095427-d-tac-yux).
func (r *Registry) CloneDir(repoID model.RepoID) (string, error) {
	return cloneDir(r.loc.DataRoot, repoID)
}

// LegacyCacheDir is where an earlier sdd kept the read-only cache clone of
// repoID, for removing it and migrating the index it may still hold.
func (r *Registry) LegacyCacheDir(repoID model.RepoID) (string, error) {
	err := model.ValidateRepoID(repoID)
	if err != nil {
		return "", err
	}
	return filepath.Join(r.loc.CacheRoot, filepath.FromSlash(string(repoID))), nil
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

// UnconnectedClone is a clone whose repository the user-global repos list
// does not name.
type UnconnectedClone struct {
	Dir    string
	RepoID model.RepoID
	// Owned marks an sdd-owned clone under <DataRoot>/repos; the others are
	// cache clones an earlier sdd kept under the cache root.
	Owned bool
}

// UnconnectedClones lists the sdd-owned clones and the leftover cache clones
// whose repository the user-global repos list does not name. A clone is a
// directory holding .git below a top-level directory named like a host: repo
// IDs open with a dotted hostname, which keeps the index and graph caches
// beside them out of the walk.
func (r *Registry) UnconnectedClones() ([]UnconnectedClone, error) {
	if r.loc.DataRoot == "" {
		return nil, errors.New("listing clones: no data root for the sdd-owned clones")
	}
	cfg, err := r.Load()
	if err != nil {
		return nil, err
	}
	owned, err := unconnectedClonesUnder(filepath.Join(r.loc.DataRoot, "repos"), cfg, true)
	if err != nil {
		return nil, err
	}
	cached, err := unconnectedClonesUnder(r.loc.CacheRoot, cfg, false)
	if err != nil {
		return nil, err
	}
	return append(owned, cached...), nil
}

func unconnectedClonesUnder(base string, cfg *GlobalConfig, owned bool) ([]UnconnectedClone, error) {
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
				stale = append(stale, UnconnectedClone{Dir: path, RepoID: repoID, Owned: owned})
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
	err := model.ValidateRepoID(repoID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dataRoot, "repos", filepath.FromSlash(string(repoID))), nil
}
