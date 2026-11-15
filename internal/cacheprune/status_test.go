package cacheprune_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/model"
)

func TestUpdateKeepsTheRunOnRecord(t *testing.T) {
	stateRoot := t.TempDir()
	started := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	removed := model.CachePruneCounts{Versions: 1, Stores: 2, Snapshots: 3, Clones: 4, Records: 5, Leftovers: 6, Bytes: 7}
	err := cacheprune.Update(t.Context(), stateRoot, func(s *model.CachePruneStatus) {
		s.StartedAt, s.FinishedAt, s.Removed, s.Error = started, started.Add(time.Minute), removed, "removing clone: permission denied"
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := cacheprune.Load(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !status.StartedAt.Equal(started) || !status.FinishedAt.Equal(started.Add(time.Minute)) || status.Removed != removed || status.Error != "removing clone: permission denied" {
		t.Errorf("the record should read back the run as written, got %+v", status)
	}
}

func TestAnUnreadableRecordReadsAsAFailedRunAndIsReplaced(t *testing.T) {
	stateRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateRoot, "cache-prune.json"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := cacheprune.Load(stateRoot)
	if err != nil {
		t.Fatalf("an unreadable record should not stop a run or sdd info: %v", err)
	}
	if status.Error == "" {
		t.Error("an unreadable record should read as a failed run for sdd info")
	}
	if err := cacheprune.Update(t.Context(), stateRoot, func(s *model.CachePruneStatus) { s.Error = "" }); err != nil {
		t.Fatalf("an update should replace an unreadable record: %v", err)
	}
	if status, err := cacheprune.Load(stateRoot); err != nil || status.Error != "" {
		t.Errorf("the replaced record should read cleanly, got %+v, %v", status, err)
	}
}
