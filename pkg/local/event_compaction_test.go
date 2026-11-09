package local_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/segment"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

// Tiered compaction merges a channel's small segments and holds each event
// once; a full compaction leaves one segment without the events the
// Settled hook names (D17).
func TestFilesystemEventStore_CompactionDropsDuplicatesAndSettledEvents(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	var settledCalls int
	store, err := local.NewFilesystemEventStore(local.FilesystemEventStoreOptions{Dir: dir, Settled: func(_ context.Context, channel string, events []sdd.Event) ([]string, error) {
		settledCalls++
		var ids []string
		for _, event := range events {
			if channel == "recheck" && event.ID == "e0" {
				ids = append(ids, event.ID)
			}
		}
		return ids, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 7 {
		event := testEvent(fmt.Sprintf("e%d", i), "recheck")
		for range 2 {
			if err := store.Append(t.Context(), []sdd.Event{event}); err != nil {
				t.Fatal(err)
			}
		}
	}
	channelDir := filepath.Join(dir, "recheck")
	if files := segmentFiles(t, channelDir); len(files) >= 8 {
		t.Fatalf("%d segments: a tier of eight small segments should have merged", len(files))
	}
	if settledCalls == 0 {
		t.Fatal("a merge should ask the Settled hook which events to drop")
	}
	if err := store.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	files := segmentFiles(t, channelDir)
	if len(files) != 1 {
		t.Fatalf("%d segments after a full compaction, want one", len(files))
	}
	data, err := os.ReadFile(filepath.Join(channelDir, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	records, err := segment.Decode(data, "sdd.events")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 6 {
		t.Fatalf("the merged segment holds %d records: each kept event should be stored once", len(records))
	}
	events, err := store.Query(t.Context(), sdd.EventQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"e1", "e2", "e3", "e4", "e5", "e6"}) {
		t.Fatalf("events %v: compaction should hold each event once and drop the settled one", ids)
	}
}
