package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/networkteam/sdd/internal/chunking"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/index"
	llmembed "github.com/networkteam/sdd/internal/llm/embed"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
)

// tokenCountingEmbedder reports one token per rune and rejects any input longer
// than tokenLimit runes, so calibration derives a cap from tokenLimit alone.
type tokenCountingEmbedder struct {
	tokenLimit int
	// failFirstCall fails the first endpoint call only, the calibration's
	// first probe, so a fill that ignores the failure would still succeed.
	failFirstCall error
	calls         int
}

func (e *tokenCountingEmbedder) Embed(_ context.Context, req embed.Request) (embed.Result, error) {
	e.calls++
	if e.failFirstCall != nil && e.calls == 1 {
		return embed.Result{}, e.failFirstCall
	}
	vectors := make([][]float32, len(req.Texts))
	tokens := 0
	for i, text := range req.Texts {
		runes := utf8.RuneCountInString(text)
		if runes > e.tokenLimit {
			return embed.Result{}, fmt.Errorf("input of %d tokens exceeds the endpoint limit", runes)
		}
		tokens += runes
		vectors[i] = []float32{1, 0, 0, 0}
	}
	return embed.Result{Vectors: vectors, Usage: llm.Usage{InputTokens: tokens}}, nil
}

func (e *tokenCountingEmbedder) Fingerprint() string { return "capped/v1/4" }

// capGraphReader serves a fixed in-memory graph to the handler.
type capGraphReader struct {
	handlers.Reader
	graph *model.Graph
}

func (r capGraphReader) CurrentGraph(string) (*model.Graph, error) { return r.graph, nil }

func capFixtureGraph() *model.Graph {
	return model.NewGraph([]*model.Entry{
		{ID: "20260101-100000-s-tac-aaa", Summary: "Summary A", Content: "Body A"},
		{ID: "20260101-110000-s-tac-bbb", Summary: "Summary B", Content: "Body B"},
	})
}

func newCapHandler(t *testing.T, indexDir, calibrationDir string, emb *tokenCountingEmbedder, endpointKey string) *handlers.IndexHandler {
	t.Helper()
	return handlers.NewIndexHandler(handlers.IndexHandlerOptions{
		GraphDir: t.TempDir(),
		IndexDir: indexDir,
		Reader:   capGraphReader{graph: capFixtureGraph()},
		Embedder: handlers.IndexEmbedder{Embedder: emb, BatchSize: 8, CalibrationDir: calibrationDir, CalibrationKey: endpointKey},
	})
}

func loadCapManifest(t *testing.T, indexDir string) *index.Manifest {
	t.Helper()
	manifest, err := index.LoadManifest(indexDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	return manifest
}

func TestIndexHandlerFillRecordsCalibratedCap(t *testing.T) {
	t.Parallel()
	indexDir := t.TempDir()
	h := newCapHandler(t, indexDir, t.TempDir(), &tokenCountingEmbedder{tokenLimit: 8192}, "endpoint-a")
	if err := h.Build(context.Background(), &command.BuildIndexCmd{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	calibrated, err := llmembed.Calibrate(context.Background(), &tokenCountingEmbedder{tokenLimit: 8192})
	if err != nil {
		t.Fatalf("Calibrate: %v", err)
	}
	if got := loadCapManifest(t, indexDir).MaxRunes; got != calibrated.MaxRunes {
		t.Errorf("manifest should record the endpoint's calibrated cap, got %d", got)
	}
}

func TestIndexHandlerFillRefillsWhenEndpointCapChanges(t *testing.T) {
	t.Parallel()
	indexDir := t.TempDir()
	calibrationDir := t.TempDir()
	build := func(t *testing.T, h *handlers.IndexHandler) (indexed, skipped int) {
		t.Helper()
		cmd := &command.BuildIndexCmd{
			OnEntryIndexed: func(string, int) { indexed++ },
			OnEntrySkipped: func(string) { skipped++ },
		}
		if err := h.Build(context.Background(), cmd); err != nil {
			t.Fatalf("Build: %v", err)
		}
		return indexed, skipped
	}

	wide := &tokenCountingEmbedder{tokenLimit: 8192}
	if indexed, _ := build(t, newCapHandler(t, indexDir, calibrationDir, wide, "endpoint-a")); indexed != 2 {
		t.Fatalf("first fill indexed %d entries, want 2", indexed)
	}
	wideCap := loadCapManifest(t, indexDir).MaxRunes

	narrow := &tokenCountingEmbedder{tokenLimit: 4096}
	if indexed, _ := build(t, newCapHandler(t, indexDir, calibrationDir, narrow, "endpoint-b")); indexed != 2 {
		t.Errorf("fill under a changed cap indexed %d entries, want every entry re-embedded", indexed)
	}
	if got := loadCapManifest(t, indexDir).MaxRunes; got == wideCap {
		t.Errorf("manifest should record the changed cap, still %d", got)
	}

	narrowAgain := &tokenCountingEmbedder{tokenLimit: 4096}
	if indexed, skipped := build(t, newCapHandler(t, indexDir, calibrationDir, narrowAgain, "endpoint-b")); indexed != 0 || skipped != 2 {
		t.Errorf("fill under an unchanged cap indexed %d and skipped %d, want all skipped", indexed, skipped)
	}
	if narrowAgain.calls != 0 {
		t.Errorf("fill with a stored calibration should make no endpoint calls, made %d", narrowAgain.calls)
	}
}

func TestIndexHandlerFillReembedsPreCapStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	indexDir := t.TempDir()
	graph := capFixtureGraph()
	seeded, err := index.LoadManifest(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range graph.Entries {
		uncapped, err := chunking.EntryStateHash(ctx, entry, chunking.DiskAttachmentReader{GraphDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		seeded.AddVersion(entry.ID, index.EntryVersion{Hash: uncapped, Fingerprint: "capped/v1/4", Derivation: index.DerivationCurrent})
	}
	if err := seeded.Save(indexDir); err != nil {
		t.Fatal(err)
	}

	emb := &tokenCountingEmbedder{tokenLimit: 8192}
	h := newCapHandler(t, indexDir, t.TempDir(), emb, "endpoint-a")
	var indexed int
	cmd := &command.BuildIndexCmd{OnEntryIndexed: func(string, int) { indexed++ }}
	if err := h.Build(ctx, cmd); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if indexed != 2 {
		t.Errorf("fill of a pre-cap store indexed %d entries, want every entry re-embedded", indexed)
	}
	calibrated, err := llmembed.Calibrate(ctx, &tokenCountingEmbedder{tokenLimit: 8192})
	if err != nil {
		t.Fatalf("Calibrate: %v", err)
	}
	if got := loadCapManifest(t, indexDir).MaxRunes; got != calibrated.MaxRunes {
		t.Errorf("fill of a pre-cap store should record the endpoint's cap, got %d", got)
	}
}

func TestIndexHandlerFillCalibrationErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// failFirstCall is the error the endpoint returns on its first call.
		failFirstCall error
		// corrupt replaces the stored calibration file after a successful fill.
		corrupt string
	}{
		{name: "probe fails", failFirstCall: errors.New("endpoint refused the connection")},
		{name: "calibration file is malformed", corrupt: "{"},
		{name: "calibration holds no usable cap", corrupt: `{"method":"bisect","max_runes":0}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			indexDir := t.TempDir()
			calibrationDir := t.TempDir()
			if tc.corrupt != "" {
				seed := newCapHandler(t, indexDir, calibrationDir, &tokenCountingEmbedder{tokenLimit: 8192}, "endpoint-a")
				if err := seed.Build(context.Background(), &command.BuildIndexCmd{}); err != nil {
					t.Fatalf("seed Build: %v", err)
				}
				replaceOnlyFile(t, calibrationDir, tc.corrupt)
			}
			h := newCapHandler(t, indexDir, calibrationDir, &tokenCountingEmbedder{tokenLimit: 8192, failFirstCall: tc.failFirstCall}, "endpoint-a")
			if err := h.Build(context.Background(), &command.BuildIndexCmd{}); err == nil {
				t.Error("fill should return the calibration error")
			}
		})
	}
}

// replaceOnlyFile overwrites the single file in dir, the one calibration a
// fill stores, without the test re-deriving the file's name.
func replaceOnlyFile(t *testing.T, dir, content string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("calibration dir holds %d files, want 1", len(entries))
	}
	if err := os.WriteFile(filepath.Join(dir, entries[0].Name()), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
