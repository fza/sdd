package local_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
	localadapter "github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

var _ relevance.BatchSizes = (*localadapter.FilesystemBatchSizes)(nil)

const sizesKey = "chat:haiku"

// openSizes opens the file as a process starting does.
func openSizes(t *testing.T, path string) *localadapter.FilesystemBatchSizes {
	t.Helper()
	sizes, err := localadapter.NewFilesystemBatchSizes(path)
	if err != nil {
		t.Fatal(err)
	}
	return sizes
}

// storeSize writes the file an earlier process left: size for sizesKey, no
// file for 0.
func storeSize(t *testing.T, path string, size int) {
	t.Helper()
	if size == 0 {
		return
	}
	err := os.WriteFile(path, fmt.Appendf(nil, "{\n  %q: %d\n}\n", sizesKey, size), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

// storedSize reads the size the file holds for sizesKey, 0 for none.
func storedSize(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	var sizes map[string]int
	err = json.Unmarshal(data, &sizes)
	if err != nil {
		t.Fatal(err)
	}
	return sizes[sizesKey]
}

// A size another process lowered is where the next one starts; Lower never
// raises one, and a smaller one another process wrote meanwhile wins.
func TestFilesystemBatchSizes_PersistsTheSmallestSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relevance", "chat-batch-sizes.json")
	first := openSizes(t, path)
	if _, ok := first.Size(sizesKey); ok {
		t.Fatal("a missing file should hold no size")
	}
	if err := first.Lower(sizesKey, 50); err != nil {
		t.Fatal(err)
	}
	if err := first.Lower(sizesKey, 80); err != nil {
		t.Fatal(err)
	}

	second := openSizes(t, path)
	if size, ok := second.Size(sizesKey); !ok || size != 50 {
		t.Fatalf("a new process should start at 50, got %d, %v", size, ok)
	}
	if err := second.Lower("chat:gpt", 25); err != nil {
		t.Fatal(err)
	}
	if err := second.Lower(sizesKey, 12); err != nil {
		t.Fatal(err)
	}
	if err := first.Lower("chat:other", 16); err != nil {
		t.Fatal(err)
	}

	third := openSizes(t, path)
	for key, want := range map[string]int{sizesKey: 12, "chat:gpt": 25, "chat:other": 16} {
		if size, _ := third.Size(key); size != want {
			t.Errorf("%s holds %d, want %d: a write should keep what the other process wrote", key, size, want)
		}
	}
}

// An unreadable file fails loud instead of starting every judge afresh.
func TestFilesystemBatchSizes_RefusesAnUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat-batch-sizes.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := localadapter.NewFilesystemBatchSizes(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("an unreadable file should be refused naming it, got %v", err)
	}
}

// The file stays one indented JSON object of size by key, sorted by key.
func TestFilesystemBatchSizes_WritesOneObjectOfSizeByKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat-batch-sizes.json")
	sizes := openSizes(t, path)
	if err := sizes.Lower(sizesKey, 50); err != nil {
		t.Fatal(err)
	}
	if err := sizes.Lower("chat:gpt", 6); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"chat:gpt\": 6,\n  \"chat:haiku\": 50\n}\n"; string(data) != want {
		t.Fatalf("the file holds %q, want %q", data, want)
	}
}

// A process opening the file holds no size below relevance.MinCarriedBatch
// from it, and opening writes nothing.
func TestFilesystemBatchSizes_CarriesNoSizeBelowTheMinimum(t *testing.T) {
	cases := []struct {
		name   string
		stored int
		// held is the size the process holds, 0 for none.
		held int
	}{
		{"the smallest size", 1, 0},
		{"the size below the minimum", relevance.MinCarriedBatch - 1, 0},
		{"the minimum", relevance.MinCarriedBatch, relevance.MinCarriedBatch},
		{"a size above the minimum", 50, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "chat-batch-sizes.json")
			storeSize(t, path, tc.stored)
			sizes := openSizes(t, path)
			held, ok := sizes.Size(sizesKey)
			if held != tc.held || ok != (tc.held > 0) {
				t.Errorf("the process holds %d, %v for a stored %d", held, ok, tc.stored)
			}
			if stored := storedSize(t, path); stored != tc.stored {
				t.Errorf("the file holds %d: opening it should write nothing", stored)
			}
		})
	}
}

// Raise lands only on the size the file still holds, and a change keeps
// what another process wrote since this one read the file.
func TestFilesystemBatchSizes_RaisesTheSizeTheFileStillHolds(t *testing.T) {
	cases := []struct {
		name string
		// meanwhile is what another process changes after this one read 20.
		meanwhile func(sizes *localadapter.FilesystemBatchSizes) error
		change    func(sizes *localadapter.FilesystemBatchSizes) error
		want      map[string]int
	}{
		{"the size still held doubles",
			func(*localadapter.FilesystemBatchSizes) error { return nil },
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Raise(sizesKey, 20, 40) },
			map[string]int{sizesKey: 40}},
		{"a size lowered meanwhile stays",
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Lower(sizesKey, 10) },
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Raise(sizesKey, 20, 40) },
			map[string]int{sizesKey: 10}},
		{"a size raised meanwhile outlives a change to another key",
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Raise(sizesKey, 20, 40) },
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Lower("chat:gpt", 25) },
			map[string]int{sizesKey: 40, "chat:gpt": 25}},
		{"a key the file does not hold stays without a size",
			func(*localadapter.FilesystemBatchSizes) error { return nil },
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Raise("chat:gpt", 0, 40) },
			map[string]int{sizesKey: 20}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "chat-batch-sizes.json")
			storeSize(t, path, 20)
			sizes, other := openSizes(t, path), openSizes(t, path)
			if err := tc.meanwhile(other); err != nil {
				t.Fatal(err)
			}
			if err := tc.change(sizes); err != nil {
				t.Fatal(err)
			}
			stored := openSizes(t, path)
			for _, name := range []string{sizesKey, "chat:gpt"} {
				got, _ := stored.Size(name)
				if got != tc.want[name] {
					t.Errorf("the file holds %d for %s, want %d", got, name, tc.want[name])
				}
				if held, _ := sizes.Size(name); held != got {
					t.Errorf("the process holds %d for %s, the file %d: a change should leave it at the file's size", held, name, got)
				}
			}
		})
	}
}

// Two processes on one file raise and halve one key without undoing each
// other, and a size below relevance.MinCarriedBatch the file held as they
// opened it counts for neither until one of them writes.
func TestFilesystemBatchSizes_TwoProcessesChangeOneKey(t *testing.T) {
	type change func(sizes *localadapter.FilesystemBatchSizes) error
	lower := func(size int) change {
		return func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Lower(sizesKey, size) }
	}
	raise := func(from, size int) change {
		return func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Raise(sizesKey, from, size) }
	}
	type step struct {
		// by is the process changing the size, a or b.
		by     string
		change change
		// file is the size the file holds after the change, held the one
		// the changing process holds; 0 for none.
		file, held int
	}
	cases := []struct {
		name string
		// stored is the size the file holds as both processes open it.
		stored int
		steps  []step
	}{
		{"of two calls answered at one size one raise lands", 20, []step{
			{"a", raise(20, 40), 40, 40},
			{"b", raise(20, 40), 40, 40},
		}},
		{"a raise leaves the halving of the size it answered at", 20, []step{
			{"b", lower(10), 10, 10},
			{"a", raise(20, 40), 10, 10},
		}},
		{"a halving stands over the other's raise", 20, []step{
			{"a", raise(20, 40), 40, 40},
			{"b", lower(10), 10, 10},
			{"a", raise(40, 80), 10, 10},
		}},
		{"a raise lands on the size the other halved to", 40, []step{
			{"a", lower(20), 20, 20},
			{"b", raise(20, 40), 40, 40},
			{"a", raise(20, 40), 40, 40},
		}},
		{"a size halved below the minimum counts for both", 20, []step{
			{"a", lower(10), 10, 10},
			{"a", lower(5), 5, 5},
			{"b", lower(10), 5, 5},
			{"b", raise(5, 10), 10, 10},
			{"a", raise(5, 10), 10, 10},
		}},
		{"a small stored size is not raised from", 4, []step{
			{"a", raise(4, 8), 4, 0},
		}},
		{"a small stored size gives way to the half of a call that broke", 1, []step{
			{"a", lower(50), 50, 50},
			{"b", lower(50), 50, 50},
			{"b", raise(50, 100), 100, 100},
			{"a", lower(25), 25, 25},
		}},
		{"a small stored size a call broke down to counts from then on", 6, []step{
			{"a", lower(6), 6, 6},
			{"a", raise(6, 12), 12, 12},
		}},
		{"a small size written after both opened the file counts for both", 4, []step{
			{"a", lower(2), 2, 2},
			{"b", raise(2, 4), 4, 4},
			{"a", raise(4, 8), 8, 8},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "chat-batch-sizes.json")
			storeSize(t, path, tc.stored)
			processes := map[string]*localadapter.FilesystemBatchSizes{"a": openSizes(t, path), "b": openSizes(t, path)}
			for i, step := range tc.steps {
				err := step.change(processes[step.by])
				if err != nil {
					t.Fatal(err)
				}
				if stored := storedSize(t, path); stored != step.file {
					t.Fatalf("step %d by %s left %d in the file, want %d", i+1, step.by, stored, step.file)
				}
				held, ok := processes[step.by].Size(sizesKey)
				if held != step.held || ok != (step.held > 0) {
					t.Fatalf("step %d left process %s holding %d, %v, want %d", i+1, step.by, held, ok, step.held)
				}
			}
		})
	}
}

// Over a stored size below relevance.MinCarriedBatch a judge starts at its
// full batch and writes a size only once a call breaks; the size it broke
// down to is its own, and the next process starts full again.
func TestFilesystemBatchSizes_AJudgeStartsFullOverASmallStoredSize(t *testing.T) {
	cases := []struct {
		name   string
		stored int
		// limit is the largest call the judge answers.
		limit      int
		candidates int
		// start is the judge's batch as the process opens the file, after
		// the one once the request returned, next the one of a process
		// opening the file then.
		start int
		calls []int
		file  int
		after int
		next  int
	}{
		{name: "calls that answer write nothing", stored: 1, limit: 100, candidates: 100, start: 100, calls: []int{100}, file: 1, after: 100, next: 100},
		{name: "a call that breaks writes its half", stored: 1, limit: 25, candidates: 100, start: 100, calls: []int{100, 50, 25, 25, 25, 25}, file: 25, after: 25, next: 25},
		{name: "a size broken down below the minimum lasts for the process", stored: 9, limit: 4, candidates: 16, start: 100, calls: []int{16, 8, 4, 4, 4, 4}, file: 4, after: 4, next: 100},
		{name: "the minimum is used as stored", stored: relevance.MinCarriedBatch, limit: 100, candidates: 30, start: 10, calls: []int{10, 10, 10}, file: 20, after: 20, next: 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "chat-batch-sizes.json")
			storeSize(t, path, tc.stored)
			var calls []int
			inner := &test.ScriptedScorer{Batch: 100, Script: func(_ context.Context, request relevance.Request) error {
				calls = append(calls, len(request.Candidates))
				if len(request.Candidates) > tc.limit {
					return &llm.MalformedAnswerError{Err: errors.New("unparsable")}
				}
				return nil
			}}
			judge := relevance.Halving(inner, openSizes(t, path), sizesKey)
			if judge.MaxBatch() != tc.start {
				t.Fatalf("the judge starts at %d, want %d", judge.MaxBatch(), tc.start)
			}
			candidates := make([]relevance.Candidate, tc.candidates)
			for i := range candidates {
				candidates[i] = relevance.Candidate{ID: fmt.Sprint(i)}
			}
			_, err := judge.Score(t.Context(), relevance.Request{Candidates: candidates})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(calls, tc.calls) {
				t.Errorf("calls of %v, want %v", calls, tc.calls)
			}
			if stored := storedSize(t, path); stored != tc.file {
				t.Errorf("the file holds %d, want %d", stored, tc.file)
			}
			if judge.MaxBatch() != tc.after {
				t.Errorf("the judge goes on at %d, want %d", judge.MaxBatch(), tc.after)
			}
			next := relevance.Halving(inner, openSizes(t, path), sizesKey)
			if next.MaxBatch() != tc.next {
				t.Errorf("the next process starts at %d, want %d", next.MaxBatch(), tc.next)
			}
		})
	}
}
