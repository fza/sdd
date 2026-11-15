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
		s.StartedAt, s.FinishedAt, s.Removed = started, started.Add(time.Minute), removed
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := cacheprune.Load(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !status.StartedAt.Equal(started) || !status.FinishedAt.Equal(started.Add(time.Minute)) || status.Removed != removed {
		t.Errorf("the record should read back the run as written, got %+v", status)
	}
}

func TestAnUnreadableRecordIsReportedAndReplaced(t *testing.T) {
	stateRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateRoot, "cache-prune.json"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := cacheprune.Load(stateRoot)
	if err != nil {
		t.Fatalf("an unreadable record should not stop a run or sdd info: %v", err)
	}
	if status.FailureNotice() == "" {
		t.Error("an unreadable record should be reported")
	}
	if err := cacheprune.Update(t.Context(), stateRoot, func(s *model.CachePruneStatus) { s.Error = "" }); err != nil {
		t.Fatalf("an update should replace an unreadable record: %v", err)
	}
	if status, err := cacheprune.Load(stateRoot); err != nil || status.Error != "" {
		t.Errorf("the replaced record should read cleanly, got %+v, %v", status, err)
	}
}

func TestTakeFailureNotice(t *testing.T) {
	stateRoot := t.TempDir()
	if notice, err := cacheprune.TakeFailureNotice(t.Context(), stateRoot); err != nil || notice != "" {
		t.Fatalf("without a record there is nothing to report, got %q, %v", notice, err)
	}
	err := cacheprune.Update(t.Context(), stateRoot, func(s *model.CachePruneStatus) {
		s.FinishedAt, s.Error = time.Now(), "removing clone: permission denied"
	})
	if err != nil {
		t.Fatal(err)
	}
	notice, err := cacheprune.TakeFailureNotice(t.Context(), stateRoot)
	if err != nil || notice == "" {
		t.Fatalf("a failed run should be reported, got %q, %v", notice, err)
	}
	if again, err := cacheprune.TakeFailureNotice(t.Context(), stateRoot); err != nil || again != "" {
		t.Errorf("a failed run should be reported once, second notice %q, %v", again, err)
	}
	status, err := cacheprune.Load(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if status.Error == "" {
		t.Error("reporting a failure should keep it on record for sdd info")
	}
}
