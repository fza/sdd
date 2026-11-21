package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	app "github.com/networkteam/sdd/pkg/application"
)

const rankingSuffix = ".ranking"

// AppendRanking keeps record as a file of its own beside the staged blobs of
// ref, <instance>.<random>.ranking, so DeleteStaged removes it with them
// (app.RankingStore).
func (s *FilesystemStagedBlobStore) AppendRanking(_ context.Context, ref app.SessionRef, instance string, record json.RawMessage) error {
	dir, err := stagingDir(ref)
	if err != nil {
		return err
	}
	err = rankingInstance(instance)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	root, err := openStoreRoot(s.locations[0].StagedBlobs, true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	err = root.MkdirAll(dir, 0o700)
	if err != nil {
		return err
	}
	id, err := randomBlobID()
	if err != nil {
		return err
	}
	return publishBytes(root, filepath.Join(dir, instance+"."+id+rankingSuffix), record)
}

func (s *FilesystemStagedBlobStore) Rankings(_ context.Context, ref app.SessionRef, instance string) ([]json.RawMessage, error) {
	dir, err := stagingDir(ref)
	if err != nil {
		return nil, err
	}
	err = rankingInstance(instance)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var records []json.RawMessage
	for _, location := range s.locations {
		kept, err := readRankings(location.StagedBlobs, dir, instance)
		if err != nil {
			return nil, err
		}
		records = append(records, kept...)
	}
	return records, nil
}

func readRankings(store, dir, instance string) ([]json.RawMessage, error) {
	root, err := openStoreRoot(store, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []json.RawMessage
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, instance+".") || !strings.HasSuffix(name, rankingSuffix) {
			continue
		}
		record, err := fs.ReadFile(root.FS(), filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// rankingInstance checks that instance can lead a record's file name, which
// its first dot ends.
func rankingInstance(instance string) error {
	err := plainSegment("ranking instance", instance)
	if err != nil {
		return err
	}
	if strings.Contains(instance, ".") {
		return fmt.Errorf("sdd: ranking instance must hold no dot, got %q", instance)
	}
	return nil
}
