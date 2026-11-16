package repos

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/model"
)

// addingPrefix names the directory GraphClones.Stage clones into.
const addingPrefix = ".adding-"

// SweepCloneStaging removes the clone staging directories a killed clone left
// under the sdd-owned clones, once nothing in them changed for longer than
// minAge, and reports how many went.
func (r *Registry) SweepCloneStaging(now time.Time, minAge time.Duration) (int, error) {
	if r.loc.DataRoot == "" {
		return 0, nil
	}
	root := filepath.Join(r.loc.DataRoot, "repos")
	var staging []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == root {
				return filepath.SkipAll
			}
			return err
		}
		if !d.IsDir() || path == root {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, model.GraphCloneStagingSuffix) || (filepath.Dir(path) == root && strings.HasPrefix(name, addingPrefix)) {
			staging = append(staging, path)
			return filepath.SkipDir
		}
		if strings.HasPrefix(name, ".") || IsCloned(path) {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("listing clone staging under %s: %w", root, err)
	}
	removed := 0
	for _, dir := range staging {
		newest, err := newestChange(dir)
		if err != nil {
			return removed, err
		}
		if now.Sub(newest) <= minAge {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return removed, fmt.Errorf("removing clone staging %s: %w", dir, err)
		}
		removed++
	}
	return removed, nil
}

// SweepLegacyHostDirs removes the host folders an earlier sdd kept its cache
// clones in under the cache root once they hold nothing but folders and lock
// files no process holds, and nothing in them changed for longer than minAge.
// It reports how many went.
func (r *Registry) SweepLegacyHostDirs(now time.Time, minAge time.Duration) (int, error) {
	hosts, err := os.ReadDir(r.loc.CacheRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("listing %s: %w", r.loc.CacheRoot, err)
	}
	removed := 0
	for _, host := range hosts {
		if !host.IsDir() || strings.HasPrefix(host.Name(), ".") || !strings.Contains(host.Name(), ".") {
			continue
		}
		gone, err := removeEmptyHostDir(filepath.Join(r.loc.CacheRoot, host.Name()), now, minAge)
		if err != nil {
			return removed, err
		}
		if gone {
			removed++
		}
	}
	return removed, nil
}

func removeEmptyHostDir(dir string, now time.Time, minAge time.Duration) (bool, error) {
	var locks []string
	empty := true
	newest := time.Time{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		switch {
		case d.IsDir() && d.Name() == ".git":
			empty = false
			return filepath.SkipAll
		case d.IsDir():
			return nil
		case strings.HasSuffix(d.Name(), ".lock"):
			locks = append(locks, path)
			return nil
		}
		empty = false
		return filepath.SkipAll
	})
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", dir, err)
	}
	if !empty || now.Sub(newest) <= minAge {
		return false, nil
	}
	for _, path := range locks {
		lock := flock.New(path)
		taken, err := lock.TryLock()
		if err != nil {
			return false, fmt.Errorf("locking %s: %w", path, err)
		}
		if !taken {
			return false, nil
		}
		defer func() { _ = lock.Unlock() }()
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, fmt.Errorf("removing %s: %w", dir, err)
	}
	return true, nil
}

// newestChange is the latest modification time of dir or anything below it.
func newestChange(dir string) (time.Time, error) {
	var newest time.Time
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("reading %s: %w", dir, err)
	}
	return newest, nil
}
