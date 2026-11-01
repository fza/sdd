package application_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

var objectGraph = map[string]string{
	"2026/07/13-030000-s-tac-gap.md": "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\ntitle: Slow reindex\nsummary: Reindexing is slow.\n---\n\nReindexing a large graph is slow.",
	"2026/07/13-040000-d-tac-fix.md": "---\ntype: decision\nkind: directive\nintent: pending\nlayer: tactical\nconfidence: high\ntitle: Batched reindex\nsummary: Reindex in batches.\nrefs:\n  - id: 20260713-030000-s-tac-gap\n    kind: addresses\n    desc: the slowness it answers\n---\n\nReindex the graph in batches of entries.",
	"2026/07/13-050000-s-tac-don.md": "---\ntype: signal\nkind: done\nlayer: tactical\nconfidence: high\ntitle: Batched reindex shipped\nsummary: Batching shipped.\ncloses:\n  - 20260713-040000-d-tac-fix\n---\n\nBatched reindex shipped.",
}

func newObjectApplication(t *testing.T) (*sdd.Application, sdd.RequestIdentity) {
	t.Helper()
	graphDir := t.TempDir()
	for rel, content := range objectGraph {
		path := filepath.Join(graphDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: "example", GraphDir: graphDir})
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
		Project: sdd.ProjectRef{ID: "example", DisplayName: "Example"}, Graph: graph,
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
	return application, sdd.RequestIdentity{Subject: "christopher", Scopes: []string{"project:read"}}
}

// Show and Search return their results as data beside the rendered text
// (20260928-080143-d-cpt-kv4).
func TestShowAndSearchReturnStructuredResults(t *testing.T) {
	application, identity := newObjectApplication(t)

	show, err := application.Show(t.Context(), identity, "example", sdd.ShowRequest{IDs: []string{"20260713-040000-d-tac-fix"}, UpDepth: 1, DownDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(show.Items) != 1 {
		t.Fatalf("show should return one item per shown entry, got %+v", show.Items)
	}
	item := show.Items[0]
	if item.ID != "20260713-040000-d-tac-fix" || item.Title != "Batched reindex" || item.Kind != "directive" || item.Layer != "tactical" {
		t.Errorf("the item should carry the entry's identity, got %+v", item)
	}
	if item.Status != "closed-by 20260713-050000-s-tac-don" {
		t.Errorf("the item should carry its derived status with the closer, got %q", item.Status)
	}
	if item.Body != "Reindex the graph in batches of entries." {
		t.Errorf("the item should carry the full body, got %q", item.Body)
	}
	if len(item.Upstream) != 1 {
		t.Fatalf("the upstream chain should hold the addressed gap, got %+v", item.Upstream)
	}
	up := item.Upstream[0]
	if up.ID != "20260713-030000-s-tac-gap" || up.Relation != "refs" || up.RefKind != "addresses" || up.Desc != "the slowness it answers" || up.Depth != 1 || up.Kind != "gap" || up.Status != "open" {
		t.Errorf("an upstream item should carry the edge and the target, got %+v", up)
	}
	if len(item.Downstream) != 1 || item.Downstream[0].ID != "20260713-050000-s-tac-don" || item.Downstream[0].Relation == "" {
		t.Errorf("the downstream chain should hold the closing done with its relation, got %+v", item.Downstream)
	}

	search, err := application.Search(t.Context(), identity, "example", sdd.SearchRequest{SyncMode: sdd.SearchSyncAll, Terms: []string{"batches"}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Hits) == 0 {
		t.Fatal("search should return its matches as hits")
	}
	hit := search.Hits[0]
	if hit.ID != "20260713-040000-d-tac-fix" || hit.Title != "Batched reindex" || hit.Status != "closed-by 20260713-050000-s-tac-don" || hit.Summary != "Reindex in batches." {
		t.Errorf("a hit should carry the entry's identity, status and summary, got %+v", hit)
	}
}
