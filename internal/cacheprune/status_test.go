package cacheprune_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/model"
)

func TestUpdateKeepsTheRunOnRecord(t *testing.T) {
	stateRoot := t.TempDir()
	started := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	removed := model.CachePruneCounts{Versions: 1, Stores: 2, Snapshots: 3, Clones: 4, Records: 5, Leftovers: 6, Sessions: 8, Unreadable: 9, Bytes: 7}
	err := cacheprune.Update(t.Context(), stateRoot, func(s *model.CachePruneStatus) {
		s.LaunchedAt, s.StartedAt, s.FinishedAt = started.Add(-time.Second), started, started.Add(time.Minute)
		s.Removed, s.Error, s.Reported = removed, "removing clone: permission denied", true
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := cacheprune.Load(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := model.CachePruneStatus{
		LaunchedAt: started.Add(-time.Second), StartedAt: started, FinishedAt: started.Add(time.Minute),
		Removed: removed, Error: "removing clone: permission denied", Reported: true,
	}
	if !status.LaunchedAt.Equal(want.LaunchedAt) || !status.StartedAt.Equal(want.StartedAt) || !status.FinishedAt.Equal(want.FinishedAt) ||
		status.Removed != want.Removed || status.Error != want.Error || status.Reported != want.Reported {
		t.Errorf("the record should read back the run as written, got %+v", status)
	}
}

func TestLaunchIfDue(t *testing.T) {
	stateRoot := t.TempDir()
	now := time.Now()
	launches := 0
	launch := func() error { launches++; return nil }

	for range 3 {
		if err := cacheprune.LaunchIfDue(t.Context(), stateRoot, now, launch); err != nil {
			t.Fatal(err)
		}
	}
	if launches != 1 {
		t.Errorf("a burst of commands should launch one run, launched %d", launches)
	}
	if err := cacheprune.LaunchIfDue(t.Context(), stateRoot, now.Add(model.CachePruneInterval), launch); err != nil {
		t.Fatal(err)
	}
	if launches != 2 {
		t.Errorf("a day later another run should launch, launched %d", launches)
	}

	failing := func() error { return errors.New("fork failed") }
	if err := cacheprune.LaunchIfDue(t.Context(), stateRoot, now.Add(3*model.CachePruneInterval), failing); err != nil {
		t.Fatal(err)
	}
	notice, err := cacheprune.TakeFailureNotice(t.Context(), stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice, "fork failed") {
		t.Errorf("a launch that fails should be reported once like a failed run, notice %q", notice)
	}
}

func TestLaunchIfDueLaunchesOnceForConcurrentCommands(t *testing.T) {
	stateRoot := t.TempDir()
	now := time.Now()
	var launches atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-start
			err := cacheprune.LaunchIfDue(t.Context(), stateRoot, now, func() error { launches.Add(1); return nil })
			if err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	if got := launches.Load(); got != 1 {
		t.Errorf("commands reading the run as due at once should launch one run, launched %d", got)
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
	if err != nil || !strings.Contains(notice, "permission denied") {
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
