package repos

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
)

// RemoteURLGit reads the remote a clone fetches from.
type RemoteURLGit interface {
	RemoteURL(ctx context.Context, dir string) (string, error)
}

// CloneTargets lists the remotes of the sdd-owned clones under
// <DataRoot>/repos, the clones the git gate shares fetch slices among
// (gitgate.ClonesFunc). A clone's remote is read again only when its git
// config changes.
type CloneTargets struct {
	root string
	git  RemoteURLGit

	mu     sync.Mutex
	cached map[string]cachedTarget
}

type cachedTarget struct {
	modified time.Time
	target   gitgate.Target
}

func NewCloneTargets(loc Locations, git RemoteURLGit) *CloneTargets {
	return &CloneTargets{root: filepath.Join(loc.DataRoot, "repos"), git: git, cached: map[string]cachedTarget{}}
}

// Targets lists the clones of remote hosts; a clone of a local path is never
// limited, so it is left out.
func (c *CloneTargets) Targets(ctx context.Context) ([]gitgate.Target, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var targets []gitgate.Target
	err := filepath.WalkDir(c.root, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == c.root {
			return filepath.SkipAll
		}
		if err != nil || !d.IsDir() {
			return err
		}
		if strings.HasSuffix(d.Name(), model.GraphCloneStagingSuffix) || strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		config, statErr := os.Stat(filepath.Join(path, ".git", "config"))
		if statErr != nil {
			if _, gitErr := os.Stat(filepath.Join(path, ".git")); gitErr == nil {
				return filepath.SkipDir
			}
			return nil
		}
		target, err := c.target(ctx, path, config.ModTime())
		if err != nil {
			return err
		}
		if target.Protocol != gitgate.Local {
			targets = append(targets, target)
		}
		return filepath.SkipDir
	})
	if err != nil {
		return nil, fmt.Errorf("listing clones under %s: %w", c.root, err)
	}
	return targets, nil
}

func (c *CloneTargets) target(ctx context.Context, dir string, modified time.Time) (gitgate.Target, error) {
	if cached, ok := c.cached[dir]; ok && cached.modified.Equal(modified) {
		return cached.target, nil
	}
	url, err := c.git.RemoteURL(ctx, dir)
	if err != nil {
		return gitgate.Target{}, err
	}
	target, err := gitgate.TargetFromURL(url)
	if err != nil {
		return gitgate.Target{}, err
	}
	c.cached[dir] = cachedTarget{modified: modified, target: target}
	return target, nil
}
