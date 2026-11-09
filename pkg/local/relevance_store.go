package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
)

// FilesystemRelevanceStore keeps the relevance verdict ledger in a directory
// of immutable segment files <time>-<random>.seg, one per append, merged by
// tiered compaction (20261109-004506-d-cpt-udz, D17).
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

// Append writes data as a new segment, then merges the smallest tier once
// it overflows.
func (s *FilesystemRelevanceStore) Append(ctx context.Context, data []byte) error {
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
	return compactDir(ctx, s.dir, false, mergeRelevanceSegments)
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

// Compact merges every segment into one canonical segment holding each
// record once; a directory already in that state stays untouched.
func (s *FilesystemRelevanceStore) Compact(ctx context.Context) error {
	return compactDir(ctx, s.dir, true, mergeRelevanceSegments)
}

func mergeRelevanceSegments(segments [][]byte) ([]byte, error) {
	ledger, err := relevance.Fold(segments)
	if err != nil {
		return nil, err
	}
	batch := ledger.Batch()
	if batch.Empty() {
		return nil, nil
	}
	return relevance.Encode(batch), nil
}

// SplitRelevanceStore keeps reader verdicts in one store and filter scores
// and drift steps in another: the layout of a committed ledger under
// relevance.content: reader, where only reader verdicts reach the
// repository (D20).
type SplitRelevanceStore struct {
	readers *FilesystemRelevanceStore
	scores  *FilesystemRelevanceStore
}

// NewSplitRelevanceStore returns the store writing reader verdicts to
// readers and the rest to scores.
func NewSplitRelevanceStore(readers, scores *FilesystemRelevanceStore) (*SplitRelevanceStore, error) {
	if readers == nil || scores == nil || readers.dir == scores.dir {
		return nil, fmt.Errorf("sdd: a split relevance store needs two distinct directories")
	}
	return &SplitRelevanceStore{readers: readers, scores: scores}, nil
}

func (s *SplitRelevanceStore) Append(ctx context.Context, data []byte) error {
	batch, err := relevance.Decode(data)
	if err != nil {
		return err
	}
	readers := model.RelevanceBatch{Readers: batch.Readers}
	rest := model.RelevanceBatch{Scores: batch.Scores, Steps: batch.Steps}
	if !readers.Empty() {
		err = s.readers.Append(ctx, relevance.Encode(readers))
		if err != nil {
			return err
		}
	}
	if rest.Empty() {
		return nil
	}
	return s.scores.Append(ctx, relevance.Encode(rest))
}

func (s *SplitRelevanceStore) Segments(ctx context.Context) ([][]byte, error) {
	readers, err := s.readers.Segments(ctx)
	if err != nil {
		return nil, err
	}
	scores, err := s.scores.Segments(ctx)
	if err != nil {
		return nil, err
	}
	return append(readers, scores...), nil
}

// Compact compacts both stores.
func (s *SplitRelevanceStore) Compact(ctx context.Context) error {
	return errors.Join(s.readers.Compact(ctx), s.scores.Compact(ctx))
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
