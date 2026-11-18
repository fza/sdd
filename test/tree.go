package test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// TreeSnapshot maps every path under root, root included, to its mode,
// modification time and, for a file, a hash of its bytes.
func TreeSnapshot(t testing.TB, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		state := fmt.Sprintf("%s %s", info.Mode(), info.ModTime().UTC().Format(time.RFC3339Nano))
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			state += " " + hex.EncodeToString(sum[:])
		}
		snapshot[filepath.ToSlash(rel)] = state
		return nil
	})
	if err != nil {
		t.Fatalf("reading the tree under %s: %v", root, err)
	}
	return snapshot
}

// TreeChanges names the paths added, removed or changed between two
// TreeSnapshot results, sorted.
func TreeChanges(before, after map[string]string) []string {
	var changed []string
	for path, state := range before {
		now, kept := after[path]
		switch {
		case !kept:
			changed = append(changed, "removed "+path)
		case now != state:
			changed = append(changed, "changed "+path)
		}
	}
	for path := range after {
		if _, had := before[path]; !had {
			changed = append(changed, "added "+path)
		}
	}
	slices.Sort(changed)
	return changed
}
