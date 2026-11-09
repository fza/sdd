package local_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
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

// Two branches each adding ledger segments merge with no conflict and no
// merge driver (D17).
func TestRelevanceSegments_TwoBranchesMergeWithoutConflict(t *testing.T) {
	repo := newGitRepository(t)
	dir := filepath.Join(repo.root, ".sdd", "relevance")
	store, err := local.NewFilesystemRelevanceStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	commitSegment := func(i int) {
		t.Helper()
		if err := store.Append(t.Context(), relevance.Encode(readerBatch(i))); err != nil {
			t.Fatal(err)
		}
		repo.write(fmt.Sprintf("notes/%d.md", i), "entry\n")
		if err := (git.CLI{Dir: repo.root}).Commit("sdd: capture", filepath.Join(repo.root, fmt.Sprintf("notes/%d.md", i))); err != nil {
			t.Fatal(err)
		}
	}
	commitSegment(0)
	repo.git("checkout", "-q", "-b", "other")
	commitSegment(1)
	repo.git("checkout", "-q", "main")
	commitSegment(2)
	repo.git("merge", "-q", "--no-edit", "other")
	if attributes := repo.git("check-attr", "merge", "--", ".sdd/relevance/x.seg"); !strings.HasSuffix(attributes, "unspecified") {
		t.Fatalf("a ledger segment should need no merge driver: %s", attributes)
	}
	if status := repo.git("status", "--porcelain"); status != "" {
		t.Fatalf("the merge left changes behind:\n%s", status)
	}
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(ledger.Pairs()); got != 3 {
		t.Fatalf("%d pairs after the merge: both branches' segments should be kept", got)
	}
	tracked := strings.Fields(repo.git("ls-files", "--", ".sdd/relevance"))
	if len(tracked) != 3 || !slices.ContainsFunc(tracked, func(path string) bool { return strings.HasSuffix(path, ".seg") }) {
		t.Fatalf("tracked %v: every segment should ride the commit that followed it", tracked)
	}
}

// A compaction in a committed ledger removes tracked segments; the removals
// ride the next sdd commit with the merged segment, leaving nothing behind.
func TestRelevanceSegments_CompactionRidesTheNextCommit(t *testing.T) {
	repo := newGitRepository(t)
	dir := filepath.Join(repo.root, ".sdd", "relevance")
	store, err := local.NewFilesystemRelevanceStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		if err := store.Append(t.Context(), relevance.Encode(readerBatch(i))); err != nil {
			t.Fatal(err)
		}
		repo.write(fmt.Sprintf("notes/%d.md", i), "entry\n")
		if err := (git.CLI{Dir: repo.root}).Commit("sdd: capture", filepath.Join(repo.root, fmt.Sprintf("notes/%d.md", i))); err != nil {
			t.Fatal(err)
		}
	}
	if status := repo.git("status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Fatalf("the compaction's removals and merged segment should ride the commit:\n%s", status)
	}
	tracked := strings.Fields(repo.git("ls-files", "--", ".sdd/relevance"))
	if len(tracked) != 1 {
		t.Fatalf("tracked %v: eight segments should have merged into one", tracked)
	}
}

// Sizes of the committed ledger under relevance.content reader and all, on
// a synthetic 10k-entry ledger: each entry the subject of 12 reader
// verdicts and 60 filter scores by one judge (D20, D35).
func TestRelevanceContent_SizesOnASyntheticTenThousandEntryLedger(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a ledger of 720k records")
	}
	const entries, readers, scores = 10_000, 12, 60
	id := func(i int) string {
		return fmt.Sprintf("2026%02d%02d-%02d%02d%02d-d-tac-%03d", 1+i/2000%12, 1+i/80%25, i/3600%24, i/60%60, i%60, i%1000)
	}
	var batch model.RelevanceBatch
	for i := range entries {
		for j := 1; j <= scores && j <= i; j++ {
			pair := model.NewRelevancePair(id(i), id(i-j))
			batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: "systemone:jev-1.14.0", Direction: model.DirectionBoth, Score: uint8((i * j) % 256)}})
			if j <= readers {
				batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderCode(1 + (i+j)%3), Reason: uint8(1 + j%4)}})
			}
		}
	}
	root := t.TempDir()
	all, err := local.NewFilesystemRelevanceStore(filepath.Join(root, "all", ".sdd", "relevance"))
	if err != nil {
		t.Fatal(err)
	}
	committedReaders, err := local.NewFilesystemRelevanceStore(filepath.Join(root, "reader", ".sdd", "relevance"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := local.NewFilesystemRelevanceStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	split, err := local.NewSplitRelevanceStore(committedReaders, state)
	if err != nil {
		t.Fatal(err)
	}
	data := relevance.Encode(batch)
	for _, store := range []interface {
		Append(context.Context, []byte) error
		Compact(context.Context) error
	}{all, split} {
		if err := store.Append(t.Context(), data); err != nil {
			t.Fatal(err)
		}
		if err := store.Compact(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	allSize, readerSize := dirSize(t, all.Dir()), dirSize(t, committedReaders.Dir())
	t.Logf("committed ledger of %d entries: content all %d bytes, content reader %d bytes", entries, allSize, readerSize)
	if readerSize == 0 || readerSize >= allSize {
		t.Fatal("content reader should commit the reader verdicts alone, a part of what content all commits")
	}
	readerLedger, err := relevance.Fold(mustSegments(t, committedReaders))
	if err != nil {
		t.Fatal(err)
	}
	if got := readerLedger.Batch(); len(got.Scores) != 0 || len(got.Readers) != len(batch.Readers) {
		t.Fatal("the committed copy under content reader should hold every reader verdict and no score")
	}
}

func dirSize(t *testing.T, dir string) int64 {
	t.Helper()
	var size int64
	for _, name := range segmentFiles(t, dir) {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		size += info.Size()
	}
	return size
}

func mustSegments(t *testing.T, store *local.FilesystemRelevanceStore) [][]byte {
	t.Helper()
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return segments
}
