package local_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	"github.com/networkteam/sdd/pkg/local"
)

func readerBatch(i int) model.RelevanceBatch {
	pair := model.NewRelevancePair(fmt.Sprintf("20260101-%02d%02d00-s-tac-can", i/60, i%60), "20260601-000000-d-tac-sub")
	return model.RelevanceBatch{
		Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1}}},
		Scores:  []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: "j", Score: uint8(i)}}},
	}
}

func TestFilesystemRelevanceStore_CompactionMergesSmallSegmentsAndDropsDuplicates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "relevance")
	store, err := local.NewFilesystemRelevanceStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var want []model.RelevanceBatch
	for i := range 7 {
		for range 2 {
			if err := store.Append(t.Context(), relevance.Encode(readerBatch(i))); err != nil {
				t.Fatal(err)
			}
		}
		want = append(want, readerBatch(i))
	}
	files := segmentFiles(t, dir)
	if len(files) >= 8 {
		t.Fatalf("%d segments: a tier of eight small segments should have merged", len(files))
	}
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Batch(), model.NewRelevanceLedger(want...).Batch()) {
		t.Fatal("compaction changed what the ledger holds")
	}
	if err := store.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	files = segmentFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("%d segments after a full compaction, want one", len(files))
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	merged, err := relevance.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Readers) != 7 || len(merged.Scores) != 7 {
		t.Fatalf("merged segment holds %d readers, %d scores: duplicates should be dropped", len(merged.Readers), len(merged.Scores))
	}
	before := files[0]
	if err := store.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	if files = segmentFiles(t, dir); len(files) != 1 || files[0] != before {
		t.Fatal("compacting one canonical segment again should leave it in place")
	}
}
