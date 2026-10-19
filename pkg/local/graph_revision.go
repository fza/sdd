package local

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/filestamp"
)

// revisionCache remembers the graph directory's last revision with the stat
// stamp of every file it hashed, so an unchanged graph costs one walk and a
// stat per file instead of reading and hashing every byte. The revision value
// is exactly graphDirectoryRevision's: the lineage file records revisions
// across processes and binaries, so its definition must not change.
type revisionCache struct {
	mu       sync.Mutex
	revision string
	stamps   map[string]filestamp.Stamp
}

func (c *revisionCache) current(dir string) (string, error) {
	return c.currentAt(dir, time.Now())
}

func (c *revisionCache) currentAt(dir string, now time.Time) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	stamps, trusted, err := graphStamps(dir, now)
	if err != nil {
		return "", err
	}
	if c.revision != "" && maps.Equal(stamps, c.stamps) {
		return c.revision, nil
	}
	revision, err := graphDirectoryRevision(dir)
	if err != nil {
		return "", err
	}
	c.revision, c.stamps = "", nil
	if trusted {
		c.revision, c.stamps = revision, stamps
	}
	return revision, nil
}

// graphStamps stamps every file graphDirectoryRevision hashes. It reports
// untrusted when any stamp cannot prove its file unchanged.
func graphStamps(dir string, now time.Time) (map[string]filestamp.Stamp, bool, error) {
	stamps := map[string]filestamp.Stamp{}
	trusted := true
	err := filepath.WalkDir(dir, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() && entry.Name() == ".sdd-runtime" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Lstat(filename)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		stamp := filestamp.Of(info)
		if !stamp.Proves(now) {
			trusted = false
		}
		stamps[filename] = stamp
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return stamps, trusted, nil
}
