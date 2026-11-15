package repos

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/model"
)

// disconnectedDirName holds one empty file per repository `sdd repo remove`
// dropped while this machine connected it, beside the clones it vouches for.
// The dot keeps it out of the clone walk.
const disconnectedDirName = ".disconnected"

func disconnectedPath(cacheRoot string, repoID model.RepoID) (string, error) {
	err := model.ValidateRepoID(repoID)
	if err != nil {
		return "", err
	}
	if cacheRoot == "" {
		return "", errors.New("no cache root for the connected-repo clones")
	}
	return filepath.Join(cacheRoot, disconnectedDirName, url.PathEscape(string(repoID))), nil
}

// Disconnected reports whether `sdd repo remove` dropped repoID on this
// machine since it was last connected.
func (r *Registry) Disconnected(repoID model.RepoID) (bool, error) {
	path, err := disconnectedPath(r.loc.CacheRoot, repoID)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the disconnection record of %s: %w", repoID, err)
	}
	return true, nil
}

// MarkDisconnected records that repoID was dropped, which lets cache pruning
// remove its clone once the user-global repos list no longer names it.
func (m *Manager) MarkDisconnected(repoID model.RepoID) error {
	path, err := disconnectedPath(m.reg.loc.CacheRoot, repoID)
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	err = os.WriteFile(path, nil, 0o644)
	if err != nil {
		return fmt.Errorf("recording the disconnection of %s: %w", repoID, err)
	}
	return nil
}

// ClearDisconnected drops the record MarkDisconnected left for repoID.
func (m *Manager) ClearDisconnected(repoID model.RepoID) error {
	return clearDisconnected(m.reg.loc.CacheRoot, repoID)
}

func clearDisconnected(cacheRoot string, repoID model.RepoID) error {
	path, err := disconnectedPath(cacheRoot, repoID)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clearing the disconnection record of %s: %w", repoID, err)
	}
	return nil
}
