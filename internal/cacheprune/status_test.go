package cacheprune_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/model"
)

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
