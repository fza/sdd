package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/segment"
)

// FilesystemRelevanceStore keeps the relevance verdict ledger in a directory
// of immutable segment files <time>-<random>.seg, one per append
// (20261109-004506-d-cpt-udz, D17).
type FilesystemRelevanceStore struct {
	dir string
}

// NewFilesystemRelevanceStore returns the store over dir. Nothing is created
// until the first append.
func NewFilesystemRelevanceStore(dir string) (*FilesystemRelevanceStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("sdd: relevance store directory is required")
	}
	return &FilesystemRelevanceStore{dir: dir}, nil
}

// Dir is the directory holding the segments.
func (s *FilesystemRelevanceStore) Dir() string { return s.dir }

func (s *FilesystemRelevanceStore) Append(ctx context.Context, data []byte) error {
	err := ctx.Err()
	if err != nil {
		return err
	}
	err = os.MkdirAll(s.dir, 0o755)
	if err != nil {
		return err
	}
	return writeSegmentFile(s.dir, data)
}

func (s *FilesystemRelevanceStore) Segments(ctx context.Context) ([][]byte, error) {
	files, err := readSegmentFiles(ctx, s.dir)
	if err != nil {
		return nil, err
	}
	segments := make([][]byte, len(files))
	for i, file := range files {
		segments[i] = file.data
	}
	return segments, nil
}

// MemoryRelevanceStore is a process-local relevance ledger for tests and
// embedded compositions.
type MemoryRelevanceStore struct {
	mu       sync.Mutex
	segments [][]byte
}

// NewMemoryRelevanceStore returns an empty in-memory ledger.
func NewMemoryRelevanceStore() *MemoryRelevanceStore { return &MemoryRelevanceStore{} }

func (s *MemoryRelevanceStore) Append(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.segments = append(s.segments, bytes.Clone(data))
	return nil
}

func (s *MemoryRelevanceStore) Segments(_ context.Context) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.segments))
	for i, data := range s.segments {
		out[i] = bytes.Clone(data)
	}
	return out, nil
}

// writeSegmentFile writes data as a fresh segment in dir: a temporary file
// renamed into place, so a reader never sees a partial segment.
func writeSegmentFile(dir string, data []byte) (err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	name, err := segment.Name(time.Now())
	if err != nil {
		return err
	}
	temp := "." + name + ".tmp"
	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = root.Rename(temp, name)
	}
	if err != nil {
		return errors.Join(err, root.Remove(temp))
	}
	return nil
}

type segmentFile struct {
	path string
	name string
	data []byte
}

// readSegmentFiles returns the segments of dir in name order, none when dir
// does not exist.
func readSegmentFiles(ctx context.Context, dir string) ([]segmentFile, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []segmentFile
	for _, entry := range entries {
		if entry.IsDir() || !segment.IsName(entry.Name()) {
			continue
		}
		err := ctx.Err()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, segmentFile{path: path, name: entry.Name(), data: data})
	}
	slices.SortFunc(files, func(a, b segmentFile) int { return strings.Compare(a.name, b.name) })
	return files, nil
}
