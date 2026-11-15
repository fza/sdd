package repos

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/model"
)

// disconnectedDirName holds one file per repository `sdd repo remove`
// dropped while this machine connected it, beside the clones it vouches for.
// The dot keeps it out of the clone walk.
const disconnectedDirName = ".disconnected"

// disconnection names the config file and cache root a record was written
// under: a process reading another repos list, such as a temporary config,
// finds no record that holds for it.
type disconnection struct {
	ConfigPath string `json:"config_path"`
	CacheRoot  string `json:"cache_root"`
}

func (r *Registry) disconnection() (disconnection, error) {
	configPath, err := resolvePath(r.loc.ConfigPath)
	if err != nil {
		return disconnection{}, err
	}
	cacheRoot, err := resolvePath(r.loc.CacheRoot)
	if err != nil {
		return disconnection{}, err
	}
	return disconnection{ConfigPath: configPath, CacheRoot: cacheRoot}, nil
}

// resolvePath returns path absolute with its symlinks resolved, as far as it
// exists: a symlink retargeted later no longer names the same place.
func resolvePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("resolving %s: %w", path, err)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if errors.Is(err, fs.ErrNotExist) {
		return abs, nil
	}
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", path, err)
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

// maxEntryName keeps a repo's entry in a flat directory well inside the
// 255-byte file name limit.
const maxEntryName = 200

// entryName names repoID's entry in a flat directory: its escaped ID, or the
// hash of the ID when that outgrows a file name. A hash holds no '%', so it
// never equals an escaped ID, which always escapes a '/'.
func entryName(repoID model.RepoID) string {
	escaped := url.PathEscape(string(repoID))
	if len(escaped) <= maxEntryName {
		return escaped
	}
	sum := sha256.Sum256([]byte(repoID))
	return hex.EncodeToString(sum[:])
}

func disconnectedPath(cacheRoot string, repoID model.RepoID) (string, error) {
	err := model.ValidateRepoID(repoID)
	if err != nil {
		return "", err
	}
	if cacheRoot == "" {
		return "", errors.New("no cache root for the connected-repo clones")
	}
	return filepath.Join(cacheRoot, disconnectedDirName, entryName(repoID)), nil
}

// Disconnected reports whether `sdd repo remove` dropped repoID on this
// machine since it was last connected, under this registry's config file and
// cache root. An empty record holds nothing.
func (r *Registry) Disconnected(repoID model.RepoID) (bool, error) {
	path, err := disconnectedPath(r.loc.CacheRoot, repoID)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the disconnection record of %s: %w", repoID, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return false, nil
	}
	var recorded disconnection
	err = json.Unmarshal(data, &recorded)
	if err != nil {
		return false, fmt.Errorf("reading the disconnection record %s: %w", path, err)
	}
	own, err := r.disconnection()
	if err != nil {
		return false, err
	}
	return recorded == own, nil
}

// MarkDisconnected records that repoID was dropped, which lets cache pruning
// remove its clone once the user-global repos list no longer names it.
func (m *Manager) MarkDisconnected(repoID model.RepoID) error {
	path, err := disconnectedPath(m.reg.loc.CacheRoot, repoID)
	if err != nil {
		return err
	}
	record, err := m.reg.disconnection()
	if err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encoding the disconnection record of %s: %w", repoID, err)
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	err = os.WriteFile(path, data, 0o644)
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
