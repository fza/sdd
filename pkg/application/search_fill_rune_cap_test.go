package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/networkteam/sdd/internal/index"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

var errRuneCapCalibration = errors.New("calibration probe failed")

// newRuneCapApp serves a counter graph through the application's search path,
// the path a serve-side fill runs, with the given rune cap resolver.
func newRuneCapApp(t *testing.T, graphDir, cacheRoot string, runeCap func(context.Context) (int, error)) *sdd.Application {
	t.Helper()
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: counterProject, GraphDir: graphDir})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := localadapter.NewFilesystemSessionStoreAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStoreAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project:     sdd.ProjectRef{ID: counterProject, DisplayName: "Counter"},
		Graph:       graph,
		RuneCap:     runeCap,
		Embedder:    &countingEmbeddings{},
		SearchIndex: localadapter.NewPersistentSearchIndexStore(counterProject, cacheRoot, "counter/repo"),
		LLM: pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
			return pkgllm.Result{Identity: pkgllm.Identity{Provider: "test", Model: "test"}}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: &runtimeAccessResolver{runtime: runtime}, Sessions: sessions, StagedBlobs: blobs})
	if err != nil {
		t.Fatal(err)
	}
	return application
}

// TestServeFillHashesMatchTheCLIUnderTheSameCap checks that a serve-path fill
// stores the entry under the hash the CLI derives for that entry and cap, and
// records the cap on the store.
func TestServeFillHashesMatchTheCLIUnderTheSameCap(t *testing.T) {
	const runeCap = 512
	graphDir := t.TempDir()
	cacheRoot := t.TempDir()
	const id = "20260101-100000-s-tac-aaa"
	writeCounterEntry(t, graphDir, id, "Alpha summary", "## Section\nThe alpha entry body.")

	application := newRuneCapApp(t, graphDir, cacheRoot, func(context.Context) (int, error) { return runeCap, nil })
	counterSearch(t, application, "alpha")

	manifest, err := index.LoadManifest(counterStoreDir(cacheRoot))
	if err != nil {
		t.Fatal(err)
	}
	state := manifest.Entries[id]
	if !state.HasVersion(entryStateHashCappedOf(t, graphDir, id, runeCap), counterFingerprint) {
		t.Errorf("serve fill stored no version under the CLI hash for the calibrated cap")
	}
	if state.HasVersion(entryStateHashOf(t, graphDir, id), counterFingerprint) {
		t.Errorf("serve fill stored a version under the uncapped hash")
	}
	if manifest.MaxRunes != runeCap {
		t.Errorf("store records rune cap %d, want the calibrated cap %d", manifest.MaxRunes, runeCap)
	}
}

// TestServeFillWithoutACalibratedCapWritesNothing covers every resolver state
// that yields no usable cap: the fill must fail and leave the store untouched.
func TestServeFillWithoutACalibratedCapWritesNothing(t *testing.T) {
	cases := []struct {
		name    string
		runeCap func(context.Context) (int, error)
		wantErr error
	}{
		{name: "no resolver"},
		{name: "resolver error", runeCap: func(context.Context) (int, error) { return 0, errRuneCapCalibration }, wantErr: errRuneCapCalibration},
		{name: "resolver zero cap", runeCap: func(context.Context) (int, error) { return 0, nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			graphDir := t.TempDir()
			cacheRoot := t.TempDir()
			const id = "20260101-100000-s-tac-aaa"
			writeCounterEntry(t, graphDir, id, "Alpha summary", "## Section\nThe alpha entry body.")

			application := newRuneCapApp(t, graphDir, cacheRoot, tc.runeCap)
			_, err := application.Search(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, counterProject, sdd.SearchRequest{SyncMode: sdd.SearchSyncAll, Phrase: "alpha", Limit: 5, MaxCitations: 3})
			if err == nil {
				t.Fatal("serve fill without a calibrated cap returned no error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("serve fill error = %v, want it to wrap the resolver error", err)
			}
			manifest, err := index.LoadManifest(counterStoreDir(cacheRoot))
			if err != nil {
				t.Fatal(err)
			}
			if len(manifest.Entries) != 0 || manifest.MaxRunes != 0 {
				t.Errorf("serve fill wrote %d entries and rune cap %d, want none", len(manifest.Entries), manifest.MaxRunes)
			}
		})
	}
}
