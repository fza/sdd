package roundproc

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LogDirName is the directory under sdd's state root holding the logs of
// round processes, one directory per repository.
const LogDirName = "rounds"

// LogPath is the log in dir of the process of round number of the delegate
// child session.
func LogPath(dir, session string, number int) string {
	return filepath.Join(dir, fmt.Sprintf("%s-round-%d.log", session, number))
}

// PruneLogs removes the round logs under root unchanged for longer than
// retention, returning how many went and the bytes freed.
func PruneLogs(root string, now time.Time, retention time.Duration) (removed int, freed int64, err error) {
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if path == root && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			return nil
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) <= retention {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		removed++
		freed += info.Size()
		return nil
	})
	return removed, freed, err
}
