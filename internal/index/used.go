package index

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Last-use stamps tell background pruning which stores and derivation rules a
// binary still reads (20261101-131514-d-cpt-9ci). Each is an empty file under
// <store>/used/ named <rule>@<manifest format>, its modification time the last
// use; one file per rule keeps binaries of different rules from racing on one
// record.

const usedSubdir = "used"

// manifestFormat is the manifest schema this binary writes (Manifest.Version).
const manifestFormat = 1

// stampRefresh is how stale a stamp may grow before a use rewrites it, so a
// read path pays one stat per call and one small write a day.
const stampRefresh = 24 * time.Hour

func stampName() string {
	return fmt.Sprintf("%s@%d", DerivationCurrent, manifestFormat)
}

// markUsed refreshes this binary's last-use stamp in the store when it is
// older than stampRefresh.
func markUsed(indexDir string, now time.Time) error {
	path := filepath.Join(indexDir, usedSubdir, stampName())
	info, err := os.Stat(path)
	switch {
	case err == nil:
		if now.Sub(info.ModTime()) < stampRefresh {
			return nil
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("creating index use stamps: %w", err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return fmt.Errorf("creating index use stamp: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("creating index use stamp: %w", err)
		}
	default:
		return fmt.Errorf("reading index use stamp: %w", err)
	}
	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("refreshing index use stamp: %w", err)
	}
	return nil
}

// RuleLastUses returns, per derivation rule, the latest use any binary of
// that rule stamped in the store, across manifest formats. A store no stamping
// binary used has none.
func RuleLastUses(indexDir string) (map[string]time.Time, error) {
	entries, err := os.ReadDir(filepath.Join(indexDir, usedSubdir))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]time.Time{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing index use stamps: %w", err)
	}
	uses := map[string]time.Time{}
	for _, entry := range entries {
		rule, _, ok := strings.Cut(entry.Name(), "@")
		if !ok || entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading index use stamp: %w", err)
		}
		if info.ModTime().After(uses[rule]) {
			uses[rule] = info.ModTime()
		}
	}
	return uses, nil
}

// StoreLastUse is the latest sign that any binary used the store: its newest
// stamp, or its manifest's last write when that is later or no binary stamped
// it.
func StoreLastUse(indexDir string) (time.Time, error) {
	uses, err := RuleLastUses(indexDir)
	if err != nil {
		return time.Time{}, err
	}
	var last time.Time
	for _, at := range uses {
		if at.After(last) {
			last = at
		}
	}
	info, err := os.Stat(manifestPath(indexDir))
	if errors.Is(err, fs.ErrNotExist) {
		info, err = os.Stat(indexDir)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("reading index store age: %w", err)
	}
	if info.ModTime().After(last) {
		last = info.ModTime()
	}
	return last, nil
}
