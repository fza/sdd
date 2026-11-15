package finders_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/chunking"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/pkg/application/types"
)

const capEntryID = "20260101-100000-s-tac-aaa"

func writeCapEntry(t *testing.T) string {
	t.Helper()
	graphDir := t.TempDir()
	entryDir := filepath.Join(graphDir, "2026", "01")
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := "---\ntype: signal\nlayer: tactical\nkind: gap\nconfidence: medium\nparticipants:\n  - Test\nsummary: |-\n  A summary.\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(entryDir, "01-100000-s-tac-aaa.md"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	return graphDir
}

func TestIndexVersionsReadsStoredCap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// storedCap is the cap the manifest records.
		storedCap int
		// versionCap is the cap the stored version's hash was computed under.
		versionCap  int
		wantCurrent bool
	}{
		{name: "pre-cap store compares uncapped hashes", storedCap: 0, versionCap: 0, wantCurrent: true},
		{name: "capped store compares its recorded cap", storedCap: 2048, versionCap: 2048, wantCurrent: true},
		{name: "pre-cap store does not assume a fallback cap", storedCap: 0, versionCap: 2048, wantCurrent: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			graphDir := writeCapEntry(t)
			indexDir := t.TempDir()
			f := finders.New(finders.Options{Config: &model.PerRepoConfig{}})
			g, err := f.CurrentGraph(graphDir)
			if err != nil {
				t.Fatal(err)
			}
			target := g.ByID[capEntryID]
			if target == nil {
				t.Fatalf("entry %s not loaded", capEntryID)
			}
			hash, err := chunking.EntryStateHashCapped(ctx, target, chunking.DiskAttachmentReader{GraphDir: graphDir}, tc.versionCap)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := index.LoadManifest(indexDir)
			if err != nil {
				t.Fatal(err)
			}
			manifest.MaxRunes = tc.storedCap
			manifest.AddVersion(capEntryID, index.EntryVersion{Hash: hash, Fingerprint: "fp", Derivation: index.DerivationCurrent, IndexedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)})
			if err := manifest.Save(indexDir); err != nil {
				t.Fatal(err)
			}

			result, err := f.IndexVersions(ctx, query.IndexVersionsQuery{Label: "local", GraphDir: graphDir, IndexDir: indexDir})
			if err != nil {
				t.Fatal(err)
			}
			if got := hasCurrentGroup(result); got != tc.wantCurrent {
				t.Errorf("stored version counted as current = %v, want %v", got, tc.wantCurrent)
			}
		})
	}
}

func hasCurrentGroup(result *query.IndexVersionsResult) bool {
	for _, group := range result.Groups {
		if group.Name == index.GroupCurrent && !group.Droppable {
			return true
		}
	}
	return false
}

// publishedAt stands in for the store: it holds exactly the entry hashes the
// test marks as published.
type publishedAt map[string]bool

func (p publishedAt) EntryPublished(_ context.Context, version types.SearchEntryVersion) (bool, error) {
	return p[version.EntryHash], nil
}

func TestSearchEntriesFinderChecksPublicationAtStoredCap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// finderCap is the cap the finder reads from the store's manifest.
		finderCap int
		// storedCap is the cap the store's entry was published under.
		storedCap     int
		wantPublished bool
	}{
		{name: "pre-cap store matches uncapped publication", finderCap: 0, storedCap: 0, wantPublished: true},
		{name: "capped store matches its recorded cap", finderCap: 2048, storedCap: 2048, wantPublished: true},
		{name: "changed cap misses the publication", finderCap: 2048, storedCap: 0, wantPublished: false},
		{name: "pre-cap store misses a capped publication", finderCap: 0, storedCap: 2048, wantPublished: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			entry := &model.Entry{ID: capEntryID, Summary: "A summary.", Content: "body"}
			attachments := chunking.DiskAttachmentReader{GraphDir: t.TempDir()}
			published, err := chunking.EntryStateHashCapped(ctx, entry, attachments, tc.storedCap)
			if err != nil {
				t.Fatal(err)
			}
			finder := finders.SearchEntriesFinder{
				Graph:       model.NewGraph([]*model.Entry{entry}),
				Revision:    "rev",
				Namespace:   types.IndexNamespace{Project: "project", Fingerprint: "fp", Metric: "cosine"},
				Attachments: attachments,
				Store:       publishedAt{published: true},
				MaxRunes:    tc.finderCap,
			}
			var requirements int
			var gotPublished bool
			for requirement, err := range finder.Discover(ctx, query.SearchDiscoveryCursor{}) {
				if err != nil {
					t.Fatal(err)
				}
				requirements++
				gotPublished = requirement.Published
			}
			if requirements != 1 {
				t.Fatalf("discovered %d requirements, want 1", requirements)
			}
			if gotPublished != tc.wantPublished {
				t.Errorf("entry reported published = %v, want %v", gotPublished, tc.wantPublished)
			}
		})
	}
}
