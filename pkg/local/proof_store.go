package local

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/networkteam/sdd/internal/relevance"
)

// FilesystemProofStore keeps the proof records of the hiding rules
// (application.ProofStore) in a directory of immutable segment files, one
// per append, merged by tiered compaction.
type FilesystemProofStore struct {
	dir string
}

// NewFilesystemProofStore returns the store over dir. Nothing is created
// until the first append.
func NewFilesystemProofStore(dir string) (*FilesystemProofStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("sdd: proof store directory is required")
	}
	return &FilesystemProofStore{dir: dir}, nil
}

func (s *FilesystemProofStore) Append(ctx context.Context, data []byte) error {
	err := ctx.Err()
	if err != nil {
		return err
	}
	err = os.MkdirAll(s.dir, 0o755)
	if err != nil {
		return err
	}
	err = writeSegmentFile(s.dir, data)
	if err != nil {
		return err
	}
	return compactDir(ctx, s.dir, false, mergeProofSegments)
}

func (s *FilesystemProofStore) Segments(ctx context.Context) ([][]byte, error) {
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

func mergeProofSegments(segments [][]byte) ([]byte, error) {
	records, err := relevance.FoldProofs(segments)
	if err != nil {
		return nil, err
	}
	if len(records.Replays)+len(records.Tallies)+len(records.Scans)+len(records.Misses) == 0 {
		return nil, nil
	}
	return relevance.EncodeProofs(records)
}

// MemoryProofStore keeps proof records in memory.
type MemoryProofStore struct {
	mu       sync.Mutex
	segments [][]byte
}

// NewMemoryProofStore returns an empty in-memory proof store.
func NewMemoryProofStore() *MemoryProofStore { return &MemoryProofStore{} }

func (s *MemoryProofStore) Append(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.segments = append(s.segments, bytes.Clone(data))
	return nil
}

func (s *MemoryProofStore) Segments(_ context.Context) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.segments))
	for i, data := range s.segments {
		out[i] = bytes.Clone(data)
	}
	return out, nil
}
