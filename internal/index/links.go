package index

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// A moved graph changes its repo key; the store keeps its directory, and the
// new key becomes a relative symlink to it, so checkouts still resolving the
// old key share one store and its locks. Each link has a use marker under
// linksSubdir, named by a hash of the key and holding the key; a link unused
// for linkIdle goes with its marker. Real store directories are never swept.
const (
	linksSubdir = ".links"
	linkIdle    = 30 * 24 * time.Hour
)

// LinkKey makes the store of newKey the store of oldKey: newKey becomes a
// relative symlink to the real directory oldKey resolves to, so links never
// chain. A newKey that is a real directory is left alone, and an oldKey with
// no store links nothing.
func LinkKey(cacheRoot, newKey, oldKey string) error {
	if newKey == oldKey {
		return nil
	}
	root := filepath.Join(cacheRoot, storeSubdir)
	real, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(oldKey)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolving the index store of %s: %w", oldKey, err)
	}
	link := filepath.Join(root, filepath.FromSlash(newKey))
	info, err := os.Lstat(link)
	switch {
	case err == nil && info.Mode()&fs.ModeSymlink == 0:
		return nil
	case err == nil:
		err = os.Remove(link)
		if err != nil {
			return fmt.Errorf("replacing the index link of %s: %w", newKey, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	err = os.MkdirAll(filepath.Dir(link), 0o755)
	if err != nil {
		return fmt.Errorf("creating the index link folder of %s: %w", newKey, err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(link))
	if err != nil {
		return err
	}
	target, err := filepath.Rel(parent, real)
	if err != nil {
		return fmt.Errorf("linking the index store of %s: %w", newKey, err)
	}
	err = os.Symlink(target, link)
	if err != nil {
		return fmt.Errorf("linking the index store of %s: %w", newKey, err)
	}
	return touchLinkMarker(root, newKey)
}

// OpenStoreDir is StoreDir for a store about to be opened: an open through a
// link marks the link used, and every open removes the links unused for
// linkIdle.
func OpenStoreDir(cacheRoot, repoKey, fingerprint string) (string, error) {
	root := filepath.Join(cacheRoot, storeSubdir)
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(repoKey)))
	if err == nil && info.Mode()&fs.ModeSymlink != 0 {
		err = touchLinkMarker(root, repoKey)
		if err != nil {
			return "", err
		}
	}
	err = sweepLinks(root, time.Now())
	if err != nil {
		return "", err
	}
	return StoreDir(cacheRoot, repoKey, fingerprint), nil
}

// RealStoreDir is dir with every link resolved, dir itself when it does not
// exist yet; two store dirs naming one store resolve equal.
func RealStoreDir(dir string) string {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return real
}

func linkMarker(root, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(root, linksSubdir, hex.EncodeToString(sum[:])[:12])
}

func touchLinkMarker(root, key string) error {
	marker := linkMarker(root, key)
	err := os.MkdirAll(filepath.Dir(marker), 0o755)
	if err != nil {
		return fmt.Errorf("creating the index link markers: %w", err)
	}
	err = os.WriteFile(marker, []byte(key), 0o644)
	if err != nil {
		return fmt.Errorf("marking the index link of %s used: %w", key, err)
	}
	now := time.Now()
	return os.Chtimes(marker, now, now)
}

func sweepLinks(root string, now time.Time) error {
	markers, err := os.ReadDir(filepath.Join(root, linksSubdir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the index link markers: %w", err)
	}
	var errs []error
	for _, entry := range markers {
		marker := filepath.Join(root, linksSubdir, entry.Name())
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if now.Sub(info.ModTime()) < linkIdle {
			continue
		}
		key, err := os.ReadFile(marker)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		link := filepath.Join(root, filepath.FromSlash(string(key)))
		linkInfo, err := os.Lstat(link)
		if err == nil && linkInfo.Mode()&fs.ModeSymlink != 0 {
			err = os.Remove(link)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, fmt.Errorf("removing the unused index link of %s: %w", key, err))
				continue
			}
		}
		err = os.Remove(marker)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
