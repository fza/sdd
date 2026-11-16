package repos

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// IsCloned reports whether dir holds a git clone.
func IsCloned(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Clone is a connected repo's sdd-owned clone, the one reader of what it
// holds. Every error it returns names the repo.
type Clone struct {
	RepoID model.RepoID
	Dir    string
}

// Cloned reports whether the clone exists on this machine.
func (c Clone) Cloned() bool {
	return IsCloned(c.Dir)
}

// Config reads the repo's committed .sdd/config.yaml; the error wraps
// meta.ErrNotAnSDDProject when it has none.
func (c Clone) Config() (*model.PerRepoConfig, error) {
	cfg, err := meta.ReadCommittedConfigFS(os.DirFS(c.Dir))
	if err != nil {
		return nil, fmt.Errorf("reading config of %s: %w", c.name(), err)
	}
	return cfg, nil
}

// GraphDir resolves the repo's graph directory from its committed config,
// the conventional location when it declares none or has no config.
func (c Clone) GraphDir() (string, error) {
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

func (c Clone) name() string {
	if c.RepoID != "" {
		return string(c.RepoID)
	}
	return c.Dir
}

// LegacyIndexDir is the pre-store per-repo index location inside a former
// cache clone (<cache>/.index), kept only so `sdd init` can migrate an
// existing build into the machine-global store. Nothing writes here anymore.
func LegacyIndexDir(dir string) string {
	return filepath.Join(dir, ".index")
}
