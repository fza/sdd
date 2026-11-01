package handlers_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/filestamp"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/graphcache"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

const pruneRetention = 90 * 24 * time.Hour

type fixedHashes map[string]string

func (f fixedHashes) CurrentHashes(context.Context) (map[string]string, error) { return f, nil }

// pruneFixture is a cache root holding one of everything a run deletes and
// one of everything it keeps.
type pruneFixture struct {
	loc         repos.Locations
	usedStore   string
	unusedStore string
	keptRow     []string
	expiredRow  []string
	keptClone   string
	staleClone  string
	keptGraph   string
}

func newPruneFixture(t *testing.T, config string) pruneFixture {
	t.Helper()
	root := t.TempDir()
	f := pruneFixture{loc: repos.Locations{
		ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"),
	}}
	if err := os.WriteFile(f.loc.ConfigPath, []byte("repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"+config), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	long := now.Add(-pruneRetention - time.Hour)

	f.usedStore = filepath.Join(f.loc.CacheRoot, "index", "github.com", "acme", "kept", "aaa")
	f.unusedStore = filepath.Join(f.loc.CacheRoot, "index", "github.com", "acme", "gone", "bbb")
	f.expiredRow, f.keptRow = []string{"e#v-old#summary"}, []string{"e#v-new#summary"}
	for _, dir := range []string{f.usedStore, f.unusedStore} {
		err := index.WriteStore(t.Context(), dir, func(idx *index.Index) error {
			m := &index.Manifest{Version: 1, Entries: map[string]index.EntryState{}}
			for _, v := range []index.EntryVersion{
				{Hash: "old", Derivation: index.DerivationCurrent, ChunkIDs: f.expiredRow, IndexedAt: long.Add(-time.Hour)},
				{Hash: "new", Derivation: index.DerivationCurrent, ChunkIDs: f.keptRow, IndexedAt: long},
			} {
				row := index.Row{EntryID: "e", EntryHash: v.Hash, ChunkID: v.ChunkIDs[0], Text: "t", Body: "t", ContentHash: index.HashContent("t"), ModelFingerprint: "m", IsSummary: true, Embedding: []float32{1, 0}}
				if err := idx.UpsertEntry(context.Background(), "e", nil, []index.Row{row}); err != nil {
					return err
				}
				m.AddVersion("e", v)
			}
			return m.Save(dir)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(f.unusedStore, "used", index.DerivationCurrent+"@1"),
		filepath.Join(f.unusedStore, "manifest.json"),
	} {
		if err := os.Chtimes(path, long, long); err != nil {
			t.Fatal(err)
		}
	}

	f.keptClone = filepath.Join(f.loc.CacheRoot, "github.com", "acme", "kept")
	f.staleClone = filepath.Join(f.loc.CacheRoot, "github.com", "acme", "dropped")
	for _, dir := range []string{f.keptClone, f.staleClone} {
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	snapshots := graphcache.New(filepath.Join(f.loc.CacheRoot, "graph"))
	f.keptGraph = t.TempDir()
	goneGraph := filepath.Join(t.TempDir(), "gone")
	for _, dir := range []string{f.keptGraph, goneGraph} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		docs := finders.GraphDocuments{Entries: []finders.EntryDocument{{ID: "20261020-100000-s-tac-aaa", Raw: []byte("---\ntype: signal\n---\n")}}}
		if err := snapshots.Save(dir, map[string]filestamp.Stamp{}, docs); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(goneGraph); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f pruneFixture) handler(keep handlers.CurrentHashSource) *handlers.CacheHandler {
	return handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc), Keep: keep,
	})
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

func TestCacheHandlerPrune(t *testing.T) {
	f := newPruneFixture(t, "")
	var counts model.CachePruneCounts
	err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{OnPruned: func(c model.CachePruneCounts) { counts = c }})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	want := model.CachePruneCounts{Versions: 1, Stores: 1, Snapshots: 1, Clones: 1}
	got := counts
	got.Bytes = 0
	if got != want || counts.Bytes == 0 {
		t.Errorf("the run should remove one of each kind and count the bytes, got %+v", counts)
	}
	if exists(t, filepath.Join(f.unusedStore, "manifest.json")) || exists(t, filepath.Join(f.unusedStore, "chromem")) {
		t.Error("the unused store's contents should be gone")
	}
	if index.DocumentsSize(f.usedStore, f.expiredRow) != 0 || index.DocumentsSize(f.usedStore, f.keptRow) == 0 {
		t.Error("the used store should lose only its replaced version")
	}
	if exists(t, f.staleClone) || !exists(t, f.keptClone) {
		t.Error("only the clone no connection names should go")
	}
	if _, ok, err := graphcache.New(filepath.Join(f.loc.CacheRoot, "graph")).Load(f.keptGraph, map[string]filestamp.Stamp{}); err != nil || !ok {
		t.Errorf("the snapshot of an existing graph should stay, load says %v, %v", ok, err)
	}
	status, err := cacheprune.Load(f.loc.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if status.StartedAt.IsZero() || status.FinishedAt.IsZero() || status.Error != "" || status.Removed.Clones != 1 {
		t.Errorf("the run should be recorded as finished without error, got %+v", status)
	}
}

func TestCacheHandlerPruneKeepsTheCheckoutsCurrentVersions(t *testing.T) {
	f := newPruneFixture(t, "")
	if err := f.handler(fixedHashes{"e": "old"}).Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatal(err)
	}
	if index.DocumentsSize(f.usedStore, f.expiredRow) == 0 {
		t.Error("a version current for the checkout running the pass should stay")
	}
}

func TestCacheHandlerPruneRunsOncePerDayWhenScheduled(t *testing.T) {
	f := newPruneFixture(t, "")
	if err := cacheprune.Update(t.Context(), f.loc.StateRoot, func(s *model.CachePruneStatus) { s.StartedAt = time.Now().Add(-time.Hour) }); err != nil {
		t.Fatal(err)
	}
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{Scheduled: true}); err != nil {
		t.Fatal(err)
	}
	if !exists(t, f.staleClone) {
		t.Error("a scheduled run within a day of the last should do nothing")
	}
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatal(err)
	}
	if exists(t, f.staleClone) {
		t.Error("a run by hand should prune regardless of the last run")
	}
}

func TestCacheHandlerPruneLeavesARunningPassAlone(t *testing.T) {
	f := newPruneFixture(t, "")
	if err := os.MkdirAll(f.loc.StateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	held := flock.New(filepath.Join(f.loc.StateRoot, cacheprune.RunLockFile))
	if _, err := held.TryLock(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Unlock() }()
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{Scheduled: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{}); !errors.Is(err, handlers.ErrCachePruneRunning) {
		t.Errorf("a run by hand while another runs should be refused, got %v", err)
	}
	if !exists(t, f.staleClone) {
		t.Error("a second run while one holds the run lock should do nothing")
	}
}

func TestCacheHandlerPruneRecordsAFailure(t *testing.T) {
	f := newPruneFixture(t, "cache:\n  retention: soon\n")
	err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{})
	if err == nil {
		t.Fatal("a run on an invalid retention should fail")
	}
	status, loadErr := cacheprune.Load(f.loc.StateRoot)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if status.Error == "" || status.Reported || status.FailureNotice() == "" {
		t.Errorf("the failure should be recorded for the next command to report, got %+v", status)
	}
}
