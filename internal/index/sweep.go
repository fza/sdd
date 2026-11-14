package index

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// stagePrefix names the directory addDocuments stages rows in before it
// renames them into the store.
const stagePrefix = ".entry-"

// SweptStore is what SweepStore removed from one store.
type SweptStore struct {
	OrphanRows int
	Temps      int
	Bytes      int64
}

// SweepStore removes what a killed writer left in the store at indexDir and
// is older than minAge: rows the manifest does not name and staging
// directories. It holds the store and fill locks, since a fill renames rows in
// before its manifest names them; ErrLockBusy reports either held.
func SweepStore(indexDir string, now time.Time, minAge time.Duration) (swept SweptStore, err error) {
	release, err := tryLockStore(indexDir)
	if err != nil {
		return SweptStore{}, err
	}
	defer func() { err = errors.Join(err, release()) }()

	manifest, err := LoadManifest(indexDir)
	if err != nil {
		return SweptStore{}, err
	}
	orphans, _, err := Orphans(indexDir, manifest)
	if err != nil {
		return SweptStore{}, err
	}
	for _, path := range orphans {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return swept, err
		}
		if now.Sub(info.ModTime()) <= minAge {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return swept, fmt.Errorf("removing orphan index row: %w", err)
		}
		swept.OrphanRows++
		swept.Bytes += info.Size()
	}

	entries, err := os.ReadDir(indexDir)
	if err != nil {
		return swept, fmt.Errorf("listing index store %s: %w", indexDir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), stagePrefix) {
			continue
		}
		path := filepath.Join(indexDir, entry.Name())
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return swept, err
		}
		if now.Sub(info.ModTime()) <= minAge {
			continue
		}
		size, err := StoreSize(path)
		if err != nil {
			return swept, err
		}
		if err := os.RemoveAll(path); err != nil {
			return swept, fmt.Errorf("removing %s: %w", path, err)
		}
		swept.Temps++
		swept.Bytes += size
	}
	return swept, nil
}

// RemoveRetiredStore deletes a store directory RemoveStore emptied, lock files
// included, once it has stayed empty for longer than minAge and neither lock
// is held. It reports whether the directory went.
func RemoveRetiredStore(indexDir string, now time.Time, minAge time.Duration) (removed bool, err error) {
	info, err := os.Stat(indexDir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if now.Sub(info.ModTime()) <= minAge {
		return false, nil
	}
	release, err := tryLockStore(indexDir)
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, release()) }()
	retired, err := isRetiredStore(indexDir)
	if err != nil || !retired {
		return false, err
	}
	for _, name := range []string{".lock", ".fill.lock"} {
		if err := os.Remove(filepath.Join(indexDir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("removing retired index store %s: %w", indexDir, err)
		}
	}
	if err := os.Remove(indexDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("removing retired index store %s: %w", indexDir, err)
	}
	return true, nil
}

// tryLockStore takes the store and fill locks of indexDir without waiting.
func tryLockStore(indexDir string) (release func() error, err error) {
	var unlocks []func() error
	release = func() error {
		var errs []error
		for i := len(unlocks) - 1; i >= 0; i-- {
			errs = append(errs, unlocks[i]())
		}
		return errors.Join(errs...)
	}
	for _, l := range []*flock.Flock{lockFile(indexDir), fillLockFile(indexDir)} {
		locked, lockErr := l.TryLock()
		if lockErr != nil {
			return nil, errors.Join(fmt.Errorf("locking index store %s: %w", indexDir, lockErr), release())
		}
		if !locked {
			return nil, errors.Join(ErrLockBusy, release())
		}
		unlocks = append(unlocks, l.Unlock)
	}
	return release, nil
}
