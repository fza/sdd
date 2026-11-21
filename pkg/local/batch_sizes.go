package local

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// FilesystemBatchSizes is a relevance.BatchSizes kept as one JSON object of
// size by key in a file, so every process on the machine starts a judge at
// the size an earlier one left it at, unless that size lies below
// relevance.MinCarriedBatch.
type FilesystemBatchSizes struct {
	path  string
	mu    sync.Mutex
	sizes map[string]int
	// stale holds, by key, a size below relevance.MinCarriedBatch the file
	// held when it was opened. While the file still holds it, the key
	// counts as holding none.
	stale map[string]int
}

// NewFilesystemBatchSizes reads the sizes the file at path holds; a missing
// file holds none and is written on the first change. A size below
// relevance.MinCarriedBatch stays in the file and is not held: a judge
// starts at its full batch, and the first Lower replaces the size.
func NewFilesystemBatchSizes(path string) (*FilesystemBatchSizes, error) {
	stored, err := readBatchSizes(path)
	if err != nil {
		return nil, err
	}
	stale := map[string]int{}
	for key, size := range stored {
		if size < relevance.MinCarriedBatch {
			stale[key] = size
		}
	}
	s := &FilesystemBatchSizes{path: path, stale: stale}
	s.sizes = s.carried(stored)
	return s, nil
}

func (s *FilesystemBatchSizes) Size(key string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	size, ok := s.sizes[key]
	return size, ok
}

// Lower keeps the smaller of size and the size the file holds for key.
func (s *FilesystemBatchSizes) Lower(key string, size int) error {
	return s.change(key, size, func(held int, ok bool) bool { return !ok || size < held })
}

// Raise sets size while the file still holds from for key.
func (s *FilesystemBatchSizes) Raise(key string, from, size int) error {
	return s.change(key, size, func(held int, ok bool) bool { return ok && held == from })
}

// change reads the file anew, so what another process wrote since stands,
// and writes size for key when due says so of the size held.
func (s *FilesystemBatchSizes) change(key string, size int, due func(held int, ok bool) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := readBatchSizes(s.path)
	if err != nil {
		return err
	}
	// A stale size the file no longer holds was replaced by a process
	// whose call broke, so the size now there counts.
	for other, was := range s.stale {
		if stored[other] != was {
			delete(s.stale, other)
		}
	}
	held, ok := stored[key]
	if _, stale := s.stale[key]; stale {
		held, ok = 0, false
	}
	if due(held, ok) {
		stored[key] = size
		data, err := json.MarshalIndent(stored, "", "  ")
		if err != nil {
			return err
		}
		err = writeBatchSizes(s.path, append(data, '\n'))
		if err != nil {
			return err
		}
		delete(s.stale, key)
	}
	s.sizes = s.carried(stored)
	return nil
}

// carried is stored without the keys whose size is stale.
func (s *FilesystemBatchSizes) carried(stored map[string]int) map[string]int {
	for key := range s.stale {
		delete(stored, key)
	}
	return stored
}

func readBatchSizes(path string) (map[string]int, error) {
	sizes := map[string]int{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return sizes, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading batch sizes: %w", err)
	}
	if err := json.Unmarshal(data, &sizes); err != nil {
		return nil, fmt.Errorf("reading batch sizes %s: %w", path, err)
	}
	return sizes, nil
}

// writeBatchSizes replaces the file in one rename.
func writeBatchSizes(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("writing batch sizes: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".batch-sizes-*.tmp")
	if err != nil {
		return fmt.Errorf("writing batch sizes: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tmp.Name()))
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return errors.Join(fmt.Errorf("writing batch sizes: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing batch sizes: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing batch sizes: %w", err)
	}
	return nil
}
