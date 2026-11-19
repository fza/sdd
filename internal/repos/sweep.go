package repos

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

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
