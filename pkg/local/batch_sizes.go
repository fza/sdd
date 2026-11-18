package local

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// FilesystemBatchSizes is a relevance.BatchSizes kept as one JSON object of
// size by key in a file, so every process on the machine starts a judge at
// the size an earlier one halved it to.
type FilesystemBatchSizes struct {
	path  string
	mu    sync.Mutex
	sizes map[string]int
}

// NewFilesystemBatchSizes reads the sizes the file at path holds; a missing
// file holds none and is written on the first Lower.
func NewFilesystemBatchSizes(path string) (*FilesystemBatchSizes, error) {
	sizes, err := readBatchSizes(path)
	if err != nil {
		return nil, err
	}
	return &FilesystemBatchSizes{path: path, sizes: sizes}, nil
}

func (s *FilesystemBatchSizes) Size(key string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	size, ok := s.sizes[key]
	return size, ok
}

// Lower keeps the smaller of size and the size held for key, merged with
// what another process wrote since.
func (s *FilesystemBatchSizes) Lower(key string, size int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := readBatchSizes(s.path)
	if err != nil {
		return err
	}
	for k, v := range stored {
		if held, ok := s.sizes[k]; !ok || v < held {
			s.sizes[k] = v
		}
	}
	if held, ok := s.sizes[key]; ok && held <= size {
		return nil
	}
	s.sizes[key] = size
	data, err := json.MarshalIndent(s.sizes, "", "  ")
	if err != nil {
		return err
	}
	return writeBatchSizes(s.path, append(data, '\n'))
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
