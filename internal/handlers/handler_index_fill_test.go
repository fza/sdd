package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
)

// readBound is the longest a reader in these tests may wait for the store,
// the role cliapp's search lock bound plays in production.
const readBound = 5 * time.Second

// slowEmbedder returns deterministic vectors and, once armed, holds every call
// by delay and closes started on the first one.
type slowEmbedder struct {
	mu      sync.Mutex
	delay   time.Duration
	started chan struct{}
	once    sync.Once
}

func (e *slowEmbedder) arm(delay time.Duration) <-chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.delay, e.started = delay, make(chan struct{})
	return e.started
}

func (e *slowEmbedder) Embed(ctx context.Context, req embed.Request) (embed.Result, error) {
	e.mu.Lock()
	delay, started := e.delay, e.started
	e.mu.Unlock()
	if started != nil {
		e.once.Do(func() { close(started) })
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return embed.Result{}, ctx.Err()
		}
	}
	return embed.Result{Vectors: vectorsFor(req.Texts)}, nil
}

func (e *slowEmbedder) Fingerprint() string { return "slow/v1/4" }

func vectorsFor(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		sum := sha256.Sum256([]byte(text))
		vector := make([]float32, 4)
		for j := range vector {
			vector[j] = float32(binary.BigEndian.Uint32(sum[j*4:j*4+4]))/float32(^uint32(0)) + 0.01
		}
		out[i] = vector
	}
	return out
}

type noLLM struct{}

func (noLLM) Run(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{}, errors.New("no llm runner configured")
}

func writeGraphEntry(t *testing.T, graphDir, id, summary, body string) {
	t.Helper()
	path := filepath.Join(graphDir, id[:4], id[4:6], id[6:]+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntype: signal\nlayer: tactical\nkind: gap\nconfidence: medium\nparticipants:\n  - Test\nsummary: " + summary + "\n---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newFillHandler(graphDir, indexDir string, emb *slowEmbedder) *handlers.IndexHandler {
	return handlers.NewIndexHandler(handlers.IndexHandlerOptions{
		GraphDir: graphDir,
		IndexDir: indexDir,
		Embedder: handlers.IndexEmbedder{Embedder: emb, BatchSize: 2},
		Reader:   finders.New(finders.Options{PreflightRunner: noLLM{}, Config: &model.PerRepoConfig{}}),
		Stderr:   io.Discard,
	})
}

type searchOutcome struct {
	busy     bool
	hits     int
	err      error
	elapsed  time.Duration
	finished time.Time
}

// search runs what `sdd search --query` does against the store: the lazy
// fill, then a bounded snapshot of the index and a query on it.
func search(ctx context.Context, h *handlers.IndexHandler, indexDir string) searchOutcome {
	start := time.Now()
	var outcome searchOutcome
	outcome.err = h.LazyFill(ctx, &command.LazyFillIndexCmd{OnFillBusy: func() { outcome.busy = true }})
	if outcome.err == nil {
		var snapshot *index.SearchSnapshot
		snapshot, outcome.err = index.OpenSearchSnapshot(ctx, indexDir, readBound)
		if outcome.err == nil {
			var hits []index.Hit
			hits, outcome.err = snapshot.Query(ctx, vectorsFor([]string{"alpha"})[0], 50)
			outcome.hits = len(hits)
			outcome.err = errors.Join(outcome.err, snapshot.Close())
		}
	}
	outcome.elapsed, outcome.finished = time.Since(start), time.Now()
	return outcome
}

func TestLazyFillLeavesConcurrentSearchesUnblocked(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	graphDir, indexDir := t.TempDir(), t.TempDir()
	emb := &slowEmbedder{}
	writeGraphEntry(t, graphDir, "20260101-100000-s-tac-aaa", "Alpha orchard notes", "The alpha entry is about apple orchards.")
	if err := newFillHandler(graphDir, indexDir, emb).Build(ctx, &command.BuildIndexCmd{}); err != nil {
		t.Fatalf("seeding the index: %v", err)
	}
	for i := range 12 {
		writeGraphEntry(t, graphDir, fmt.Sprintf("20260102-1000%02d-s-tac-f%02d", i, i), fmt.Sprintf("Fill topic %d", i), fmt.Sprintf("Entry %d waits for the slow embedder.", i))
	}
	started := emb.arm(400 * time.Millisecond)

	var filler searchOutcome
	fillDone := make(chan struct{})
	go func() {
		defer close(fillDone)
		filler = search(ctx, newFillHandler(graphDir, indexDir, emb), indexDir)
	}()
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the fill made no embedding call")
	}

	searches := make([]searchOutcome, 2)
	var wg sync.WaitGroup
	for i := range searches {
		wg.Go(func() { searches[i] = search(ctx, newFillHandler(graphDir, indexDir, emb), indexDir) })
	}
	wg.Wait()
	<-fillDone

	if filler.err != nil {
		t.Fatalf("filling search failed: %v", filler.err)
	}
	if filler.busy {
		t.Error("the first search should fill, not skip")
	}
	for i, outcome := range searches {
		if outcome.err != nil {
			t.Fatalf("concurrent search %d failed: %v", i, outcome.err)
		}
		if !outcome.busy {
			t.Errorf("concurrent search %d should skip the fill another search runs", i)
		}
		if outcome.elapsed >= readBound {
			t.Errorf("concurrent search %d should return within the read bound, took %s", i, outcome.elapsed)
		}
		if !outcome.finished.Before(filler.finished) {
			t.Errorf("concurrent search %d should return while the fill still runs", i)
		}
		if outcome.hits == 0 {
			t.Errorf("concurrent search %d should find the indexed entries", i)
		}
	}

	manifest, err := index.LoadManifest(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		if id := fmt.Sprintf("20260102-1000%02d-s-tac-f%02d", i, i); len(manifest.Entries[id].Versions) == 0 {
			t.Errorf("the fill should complete, entry %s is not indexed", id)
		}
	}
}

func TestLazyFillRefreshesSearchSegments(t *testing.T) {
	graphDir, indexDir := t.TempDir(), t.TempDir()
	writeGraphEntry(t, graphDir, "20260101-100000-s-tac-aaa", "Alpha orchard notes", "The alpha entry is about apple orchards.")
	if err := newFillHandler(graphDir, indexDir, &slowEmbedder{}).LazyFill(t.Context(), &command.LazyFillIndexCmd{}); err != nil {
		t.Fatal(err)
	}
	segments, err := filepath.Glob(filepath.Join(indexDir, "segments", "*.seg"))
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) == 0 {
		t.Error("a fill should leave the rows it wrote in the search segments")
	}
}

func TestLazyFillUsesTheGivenGraph(t *testing.T) {
	graphDir, otherDir, indexDir := t.TempDir(), t.TempDir(), t.TempDir()
	writeGraphEntry(t, graphDir, "20260101-100000-s-tac-aaa", "Alpha orchard notes", "The alpha entry is about apple orchards.")
	writeGraphEntry(t, otherDir, "20260101-100001-s-tac-bbb", "Beta harbour notes", "The beta entry is about harbours.")
	given, err := finders.New(finders.Options{PreflightRunner: noLLM{}, Config: &model.PerRepoConfig{}}).CurrentGraph(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := newFillHandler(graphDir, indexDir, &slowEmbedder{}).LazyFill(t.Context(), &command.LazyFillIndexCmd{Graph: given}); err != nil {
		t.Fatal(err)
	}
	manifest, err := index.LoadManifest(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := manifest.Entries["20260101-100001-s-tac-bbb"]; !ok {
		t.Error("the fill should index the graph it was given")
	}
	if _, ok := manifest.Entries["20260101-100000-s-tac-aaa"]; ok {
		t.Error("the fill should not load the graph again")
	}
}
