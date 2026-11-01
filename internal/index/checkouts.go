package index

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Checkout records keep the versions every checkout reading a store still
// uses safe from pruning (20261101-153339-d-tac-pnl): one file per checkout
// under <store>/used/checkouts/, holding each entry's current state hash, its
// modification time the checkout's last read.

const checkoutsSubdir = "checkouts"

// KeepSet names, per entry ID, the state hashes pruning must not delete.
type KeepSet map[string]map[string]bool

// NewKeepSet holds one current hash per entry.
func NewKeepSet(hashes map[string]string) KeepSet {
	keep := KeepSet{}
	keep.add(hashes)
	return keep
}

func (k KeepSet) add(hashes map[string]string) {
	for id, hash := range hashes {
		if k[id] == nil {
			k[id] = map[string]bool{}
		}
		k[id][hash] = true
	}
}

// Has reports whether hash is a version of entry id that must stay.
func (k KeepSet) Has(id, hash string) bool { return k[id][hash] }

func checkoutRecordPath(indexDir, checkout string) string {
	sum := sha256.Sum256([]byte(checkout))
	return filepath.Join(indexDir, usedSubdir, checkoutsSubdir, hex.EncodeToString(sum[:8])+".json")
}

// RecordCheckout records hashes as the versions checkout currently reads from
// the store at indexDir. An unchanged record is only touched, at most once per
// stampRefresh, so a search pays one read and a write a day.
func RecordCheckout(indexDir, checkout string, hashes map[string]string, now time.Time) error {
	checkout, err := filepath.Abs(checkout)
	if err != nil {
		return fmt.Errorf("resolving checkout path: %w", err)
	}
	path := checkoutRecordPath(indexDir, checkout)
	data, err := json.Marshal(hashes)
	if err != nil {
		return fmt.Errorf("encoding checkout record: %w", err)
	}
	info, err := os.Stat(path)
	switch {
	case err == nil:
		existing, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("reading checkout record: %w", err)
		}
		if bytes.Equal(existing, data) {
			if now.Sub(info.ModTime()) < stampRefresh {
				return nil
			}
			if err := os.Chtimes(path, now, now); err != nil {
				return fmt.Errorf("refreshing checkout record: %w", err)
			}
			return nil
		}
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("reading checkout record: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating checkout records: %w", err)
	}
	temp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("writing checkout record: %w", err)
	}
	if err := os.Chtimes(temp, now, now); err != nil {
		return fmt.Errorf("writing checkout record: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("writing checkout record: %w", err)
	}
	return nil
}

// checkoutKeep collects the versions named by checkout records fresher than
// retention into keep, and deletes the records older than retention.
func checkoutKeep(indexDir string, now time.Time, retention time.Duration, keep KeepSet) error {
	dir := filepath.Join(indexDir, usedSubdir, checkoutsSubdir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("listing checkout records: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading checkout record: %w", err)
		}
		if now.Sub(info.ModTime()) > retention {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("removing expired checkout record: %w", err)
			}
			continue
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading checkout record: %w", err)
		}
		var hashes map[string]string
		if err := json.Unmarshal(data, &hashes); err != nil {
			return fmt.Errorf("reading checkout record %s: %w", path, err)
		}
		keep.add(hashes)
	}
	return nil
}
