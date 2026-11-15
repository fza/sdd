package handlers_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/networkteam/sdd/internal/llm/embed"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

const pruneRetention = 90 * 24 * time.Hour

type fixedHashes map[string]string

func (f fixedHashes) CurrentHashes(context.Context, int) (map[string]string, error) { return f, nil }

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
	if err := repos.NewManager(repos.NewRegistry(f.loc), nil).MarkDisconnected("github.com/acme/dropped"); err != nil {
		t.Fatal(err)
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

	calibrations := filepath.Join(f.loc.CacheRoot, embed.CalibrationDirName)
	if err := os.MkdirAll(calibrations, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"calibration-unused.json", "calibration-used.json"} {
		if err := os.WriteFile(filepath.Join(calibrations, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(calibrations, "calibration-unused.json"), long, long); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f pruneFixture) handler(keep handlers.CurrentHashSource) *handlers.CacheHandler {
	return handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc),
		Keep: keep,
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
	want := model.CachePruneCounts{Versions: 1, Stores: 1, Snapshots: 1, Clones: 1, Records: 1}
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

// usedStore writes a one-version store at dir whose last use is at.
func usedStore(t *testing.T, dir string, at time.Time) {
	t.Helper()
	err := index.WriteStore(t.Context(), dir, func(idx *index.Index) error {
		row := index.Row{EntryID: "e", EntryHash: "h", ChunkID: "e#v-h#summary", Text: "t", Body: "t", ContentHash: index.HashContent("t"), ModelFingerprint: "m", IsSummary: true, Embedding: []float32{1, 0}}
		if err := idx.UpsertEntry(context.Background(), "e", nil, []index.Row{row}); err != nil {
			return err
		}
		m := &index.Manifest{Version: 1, Entries: map[string]index.EntryState{}}
		m.AddVersion("e", index.EntryVersion{Hash: "h", Derivation: index.DerivationCurrent, ChunkIDs: []string{row.ChunkID}, IndexedAt: at})
		return m.Save(dir)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "used", index.DerivationCurrent+"@1"), filepath.Join(dir, "manifest.json")} {
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCacheHandlerPruneDropsAReplacedEmbeddingSetup(t *testing.T) {
	now := time.Now()
	replaced := 14 * 24 * time.Hour
	cases := []struct {
		name     string
		oldUse   time.Time
		newUse   *time.Time
		wantKept bool
	}{
		{name: "a store another setup replaced over two weeks ago goes", oldUse: now.Add(-replaced - time.Hour), newUse: &now},
		{name: "a store used within two weeks stays", oldUse: now.Add(-replaced + time.Hour), newUse: &now, wantKept: true},
		{name: "a store no other setup replaced keeps the unused retention", oldUse: now.Add(-replaced - time.Hour), wantKept: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPruneFixture(t, "")
			repo := filepath.Join(f.loc.CacheRoot, "index", "github.com", "acme", "switched")
			old := filepath.Join(repo, "oldsetup")
			usedStore(t, old, tc.oldUse)
			if tc.newUse != nil {
				usedStore(t, filepath.Join(repo, "newsetup"), *tc.newUse)
			}
			if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
				t.Fatal(err)
			}
			if kept := exists(t, filepath.Join(old, "manifest.json")); kept != tc.wantKept {
				t.Errorf("the older setup's store kept should be %v", tc.wantKept)
			}
		})
	}
}

func TestCacheHandlerPruneSweepsIndexLeftovers(t *testing.T) {
	f := newPruneFixture(t, "")
	old := time.Now().Add(-model.CacheLeftoverAge - time.Hour)
	retired := filepath.Join(f.loc.CacheRoot, "index", "github.com", "acme", "kept", "retired")
	if err := os.MkdirAll(retired, 0o755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(f.usedStore, ".entry-123")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(retired, ".lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{retired, stage} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	var counts model.CachePruneCounts
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{OnPruned: func(c model.CachePruneCounts) { counts = c }}); err != nil {
		t.Fatal(err)
	}
	if exists(t, retired) || exists(t, stage) {
		t.Error("a retired store and a staging dir older than a day should go")
	}
	if counts.Leftovers != 2 {
		t.Errorf("the run should count both leftovers, got %+v", counts)
	}
}

type hashesByCap map[int]map[string]string

func (h hashesByCap) CurrentHashes(_ context.Context, maxRunes int) (map[string]string, error) {
	return h[maxRunes], nil
}

func TestCacheHandlerPruneGoesOnPastADamagedStore(t *testing.T) {
	f := newPruneFixture(t, "")
	broken := filepath.Join(f.loc.CacheRoot, "index", "github.com", "acme", "broken", "ccc")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "manifest.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.handler(fixedHashes{}).Prune(t.Context(), &command.PruneCacheCmd{}); err == nil {
		t.Error("a damaged store should fail the run")
	}
	if index.DocumentsSize(f.usedStore, f.expiredRow) != 0 {
		t.Error("the stores after a damaged one should still be pruned")
	}
}

func TestCacheHandlerPruneHashesUnderEachStoresRuneCap(t *testing.T) {
	f := newPruneFixture(t, "")
	err := index.WriteStore(t.Context(), f.usedStore, func(*index.Index) error {
		m, err := index.LoadManifest(f.usedStore)
		if err != nil {
			return err
		}
		m.MaxRunes = 512
		return m.Save(f.usedStore)
	})
	if err != nil {
		t.Fatal(err)
	}
	keep := hashesByCap{512: {"e": "old"}}
	if err := f.handler(keep).Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatal(err)
	}
	if index.DocumentsSize(f.usedStore, f.expiredRow) == 0 {
		t.Error("the checkout's current versions should be hashed under the rune cap the store was chunked under")
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
		t.Errorf("a scheduled run while another runs should leave quietly, got %v", err)
	}
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{}); !errors.Is(err, handlers.ErrCachePruneRunning) {
		t.Errorf("a run by hand while another runs should be refused, got %v", err)
	}
	if !exists(t, f.staleClone) {
		t.Error("a second run while one holds the run lock should do nothing")
	}
	status, err := cacheprune.Load(f.loc.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !status.StartedAt.IsZero() {
		t.Errorf("a run refused the run lock should record no start, got %+v", status)
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

func TestCacheHandlerPruneRecordsAFailure(t *testing.T) {
	for _, tc := range []struct {
		name         string
		scheduled    bool
		wantReported bool
	}{
		{name: "by hand", wantReported: true},
		{name: "scheduled", scheduled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPruneFixture(t, "cache:\n  retention: soon\n")
			err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{Scheduled: tc.scheduled})
			if err == nil {
				t.Fatal("a run on an invalid retention should fail")
			}
			status, loadErr := cacheprune.Load(f.loc.StateRoot)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if status.Error == "" || !strings.Contains(status.Error, "cache.retention") {
				t.Errorf("the failure should be recorded for sdd info, got %+v", status)
			}
			// A run by hand showed its failure as the command's error; a
			// scheduled one leaves it for the next command to report.
			if status.Reported != tc.wantReported {
				t.Errorf("the failure should be marked reported: %v", tc.wantReported)
			}
		})
	}
}

// A replaced setup's store keeps its versions while it waits out the replaced
// retention: pruning them would save its manifest and count as a use.
func TestCacheHandlerPruneLeavesAReplacedSetupsVersionsAlone(t *testing.T) {
	now := time.Now()
	f := newPruneFixture(t, "")
	repo := filepath.Join(f.loc.CacheRoot, "index", "github.com", "acme", "switched")
	old := filepath.Join(repo, "oldsetup")
	lastUse := now.Add(-24 * time.Hour)
	err := index.WriteStore(t.Context(), old, func(idx *index.Index) error {
		m := &index.Manifest{Version: 1, Entries: map[string]index.EntryState{}}
		for _, v := range []struct {
			hash string
			at   time.Time
		}{{"h1", now.Add(-40 * 24 * time.Hour)}, {"h2", now.Add(-20 * 24 * time.Hour)}} {
			row := index.Row{EntryID: "e", EntryHash: v.hash, ChunkID: "e#v-" + v.hash + "#summary", Text: "t", Body: "t", ContentHash: index.HashContent("t"), ModelFingerprint: "m", IsSummary: true, Embedding: []float32{1, 0}}
			if err := idx.UpsertEntry(context.Background(), "e", nil, []index.Row{row}); err != nil {
				return err
			}
			m.AddVersion("e", index.EntryVersion{Hash: v.hash, Derivation: index.DerivationCurrent, ChunkIDs: []string{row.ChunkID}, IndexedAt: v.at})
		}
		return m.Save(old)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(old, "used", index.DerivationCurrent+"@1"), filepath.Join(old, "manifest.json")} {
		if err := os.Chtimes(path, lastUse, lastUse); err != nil {
			t.Fatal(err)
		}
	}
	usedStore(t, filepath.Join(repo, "newsetup"), now)
	if err := f.handler(nil).Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatal(err)
	}
	m, err := index.LoadManifest(old)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(m.Entries["e"].Versions); got != 2 {
		t.Errorf("a replaced setup's store should keep its versions, %d left", got)
	}
	last, err := index.StoreLastUse(old)
	if err != nil {
		t.Fatal(err)
	}
	if last.After(lastUse.Add(time.Second)) {
		t.Errorf("pruning should not count as a use of the replaced setup's store, last use moved to %v", last)
	}
}

// A clone the loaded repos list lacks stays unless `sdd repo remove` dropped
// it: a process reading another config root must not empty this machine's
// clones.
func TestCacheHandlerPruneKeepsAClonesNoRepoRemoveDropped(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{
		ConfigPath: filepath.Join(root, "foreign", "config.yaml"), CacheRoot: filepath.Join(root, "cache"),
		StateRoot: filepath.Join(root, "state"),
	}
	clone := filepath.Join(loc.CacheRoot, "github.com", "acme", "graph")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc)})
	if err := h.Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if !exists(t, clone) {
		t.Error("a clone no repo remove dropped should stay whatever the loaded repos list holds")
	}
}

// A dropped clone meeting a missing config file fails the run, recorded for
// sdd info, and stays.
func TestCacheHandlerPruneRefusesAMissingConfig(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state")}
	clone := filepath.Join(loc.CacheRoot, "github.com", "acme", "dropped")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := repos.NewManager(repos.NewRegistry(loc), nil).MarkDisconnected("github.com/acme/dropped"); err != nil {
		t.Fatal(err)
	}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc)})
	err := h.Prune(t.Context(), &command.PruneCacheCmd{})
	if !errors.Is(err, repos.ErrClonePassRefused) {
		t.Errorf("the run should fail with the refused clone pass, got %v", err)
	}
	if !exists(t, clone) {
		t.Error("a refused clone pass should remove nothing")
	}
	status, err := cacheprune.Load(loc.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if status.Error == "" {
		t.Error("the refusal should be recorded for sdd info")
	}
}

// A dropped clone holding a connected one stays with it, and the run goes on.
func TestCacheHandlerPruneKeepsADroppedCloneHoldingAConnectedOne(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state")}
	config := "repos:\n  - repo_id: git.example.com/foo/bar\n    clone_url: git@git.example.com:foo/bar.git\n"
	if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	outer := filepath.Join(loc.CacheRoot, "git.example.com", "foo")
	inner := filepath.Join(outer, "bar")
	for _, dir := range []string{outer, inner} {
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.NewManager(repos.NewRegistry(loc), nil).MarkDisconnected("git.example.com/foo"); err != nil {
		t.Fatal(err)
	}
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{CacheRoot: loc.CacheRoot, StateRoot: loc.StateRoot, Registry: repos.NewRegistry(loc)})
	if err := h.Prune(t.Context(), &command.PruneCacheCmd{}); err != nil {
		t.Fatalf("a clone kept for the clone it holds should not fail the run: %v", err)
	}
	if !exists(t, filepath.Join(inner, ".git")) || !exists(t, filepath.Join(outer, ".git")) {
		t.Error("a dropped clone holding a connected one should stay, and the connected one with it")
	}
}
