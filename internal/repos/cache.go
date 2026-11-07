package repos

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// Connected-repo caches are managed read-only clones under the Locations
// cache root (default $XDG_CACHE_HOME/sdd/<repo-id>/): lazy clone on first
// need, a pull on reads and via `sdd repo sync` whenever the git gate admits
// it — all lifecycle on Manager. The helpers here are pure reads over a given cache
// dir; none touch ambient state. The clone is never written to except the
// per-repo search index at <dir>/.index/.

// IsCloned reports whether the cache dir holds a git clone.
func IsCloned(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Cache is a connected repo's read-only clone, the one reader of what a cache
// holds. Every error it returns names the repo.
type Cache struct {
	RepoID model.RepoID
	Dir    string
}

// Cloned reports whether the cache holds a git clone.
func (c Cache) Cloned() bool {
	return IsCloned(c.Dir)
}

// Config reads the cached repo's committed .sdd/config.yaml; the error wraps
// meta.ErrNotAnSDDProject when it has none.
func (c Cache) Config() (*model.PerRepoConfig, error) {
	cfg, err := meta.ReadCommittedConfigFS(os.DirFS(c.Dir))
	if err != nil {
		return nil, fmt.Errorf("reading config of %s: %w", c.name(), err)
	}
	return cfg, nil
}

// GraphDir resolves the cached repo's graph directory from its committed
// config, the conventional location when it declares none or has no config.
func (c Cache) GraphDir() (string, error) {
	graphDir := model.DefaultGraphDir
	cfg, err := c.Config()
	switch {
	case errors.Is(err, meta.ErrNotAnSDDProject):
	case err != nil:
		return "", err
	case cfg.GraphDir != "":
		graphDir = cfg.GraphDir
	}
	return filepath.Join(c.Dir, filepath.FromSlash(graphDir)), nil
}

func (c Cache) name() string {
	if c.RepoID != "" {
		return string(c.RepoID)
	}
	return c.Dir
}

// LegacyIndexDir is the pre-store per-repo index location inside a clone
// cache (<cache>/.index) — kept only so `sdd init` can migrate an existing
// build into the machine-global store. Nothing writes here anymore.
func LegacyIndexDir(dir string) string {
	return filepath.Join(dir, ".index")
}
