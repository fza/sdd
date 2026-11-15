package repos

import (
	"bytes"
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
const disconnectedDirName = ".disconnected"

// disconnection names the config file and roots a record was written under:
// a process reading another repos list, such as a temporary config, finds no
// record that holds for it.
type disconnection struct {
	ConfigPath string `json:"config_path"`
	DataRoot   string `json:"data_root"`
	CacheRoot  string `json:"cache_root"`
}

func (r *Registry) disconnection() (disconnection, error) {
	var record disconnection
	for _, field := range []struct {
		path string
		into *string
	}{{r.loc.ConfigPath, &record.ConfigPath}, {r.loc.DataRoot, &record.DataRoot}, {r.loc.CacheRoot, &record.CacheRoot}} {
		resolved, err := resolvePath(field.path)
		if err != nil {
			return disconnection{}, err
		}
		*field.into = resolved
	}
	return record, nil
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
	dir, err := resolvePath(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

func disconnectedPath(dataRoot string, repoID model.RepoID) (string, error) {
	err := model.ValidateRepoID(repoID)
	if err != nil {
		return "", err
	}
	if dataRoot == "" {
		return "", errors.New("no data root for the sdd-owned clones")
	}
	return filepath.Join(dataRoot, "repos", disconnectedDirName, url.PathEscape(string(repoID))), nil
}

// Disconnected reports whether `sdd repo remove` dropped repoID on this
// machine since it was last connected, under this registry's config file and
// roots. An empty record holds nothing.
func (r *Registry) Disconnected(repoID model.RepoID) (bool, error) {
	path, err := disconnectedPath(r.loc.DataRoot, repoID)
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
		return false, fmt.Errorf("reading the disconnection record %s: %w; delete it to keep the clone of %s", path, err, repoID)
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
	path, err := disconnectedPath(m.reg.loc.DataRoot, repoID)
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
	return clearDisconnected(m.reg.loc.DataRoot, repoID)
}

func clearDisconnected(dataRoot string, repoID model.RepoID) error {
	path, err := disconnectedPath(dataRoot, repoID)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clearing the disconnection record of %s: %w", repoID, err)
	}
	return nil
}
