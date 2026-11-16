package meta

import (
	"os"
	"path/filepath"
	"time"
)

// The background-sync last-fetch marker, kept until every caller has moved
// to the git gate's record.

// LastFetchPath returns the path to the background-sync last-fetch marker.
// The file's mtime records the last time sdd attempted git fetch; its
// presence short-circuits further fetches within the configured cooldown.
func LastFetchPath(sddDir string) string {
	return filepath.Join(TmpDir(sddDir), "last-fetch")
}

// ReadLastFetch returns the mtime of the last-fetch marker, or the zero
// time if the marker does not exist. A zero result means "never fetched,"
// treat as stale.
func ReadLastFetch(sddDir string) (time.Time, error) {
	info, err := os.Stat(LastFetchPath(sddDir))
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// TouchLastFetch stamps the last-fetch marker with the current time,
// creating the .sdd/tmp/ directory and the file itself as needed.
func TouchLastFetch(sddDir string) error {
	if err := os.MkdirAll(TmpDir(sddDir), 0o755); err != nil {
		return err
	}
	path := LastFetchPath(sddDir)
	now := time.Now()
	if _, err := os.Stat(path); err == nil {
		return os.Chtimes(path, now, now)
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}
