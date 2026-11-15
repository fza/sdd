package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/presenters"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

var objectGraph = map[string]string{
	"2026/07/13-030000-s-tac-gap.md": "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nsummary: Reindexing is slow.\n---\n\nReindexing a large graph is slow.",
	"2026/07/13-040000-d-tac-fix.md": "---\ntype: decision\nkind: directive\nintent: pending\nlayer: tactical\nconfidence: high\nsummary: Reindex in batches.\nrefs:\n  - id: 20260713-030000-s-tac-gap\n    kind: addresses\n    desc: the slowness it answers\n---\n\nReindex the graph in batches of entries.",
	"2026/07/13-050000-s-tac-don.md": "---\ntype: signal\nkind: done\nlayer: tactical\nconfidence: high\nsummary: Batching shipped.\ncloses:\n  - 20260713-040000-d-tac-fix\n---\n\nBatched reindex shipped.",
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
	if item.ID != "20260713-040000-d-tac-fix" || item.Kind != "directive" || item.Layer != "tactical" {
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
	if hit.ID != "20260713-040000-d-tac-fix" || hit.Status != "closed-by 20260713-050000-s-tac-don" || hit.Summary != "Reindex in batches." {
		t.Errorf("a hit should carry the entry's identity, status and summary, got %+v", hit)
	}
}

// The text fields stay for every caller reading them, rendered by the shared
// renderer from the typed data returned beside them.
func TestTextFieldsRenderTheTypedResults(t *testing.T) {
	document := func(path, kind, summary string) sdd.EntryDocument {
		return sdd.EntryDocument{
			LogicalPath: path,
			Frontmatter: map[string]any{"type": "signal", "kind": kind, "layer": "tactical", "summary": summary},
			Body:        summary + " The body says more.",
		}
	}
	snapshot := func(project sdd.ProjectID, documents ...sdd.EntryDocument) *sdd.Snapshot {
		s, err := sdd.BuildSnapshot(t.Context(), sdd.SnapshotData{Project: project, Revision: "r1", Entries: documents})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	llm := pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
		return pkgllm.Result{Identity: pkgllm.Identity{Provider: "test", Model: "test"}}, nil
	})
	dependency, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: "example.org/dep"}, LLM: llm,
		Graph: staticGraphStore{snapshot: snapshot("example.org/dep", document("2026/07/13-040000-s-tac-dep.md", "gap", "Dependency kestrel gap."))},
	})
	if err != nil {
		t.Fatal(err)
	}
	base, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: "base"}, Dependencies: []sdd.RepoID{"example.org/dep"}, LLM: llm,
		Graph: staticGraphStore{snapshot: snapshot("base",
			document("2026/07/13-030000-s-tac-gap.md", "gap", "Local kestrel gap."),
			document("2026/07/13-050000-s-tac-ins.md", "insight", "Local kestrel insight."),
		)},
	})
	if err != nil {
		t.Fatal(err)
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: &multiAccessResolver{base: base, dependency: dependency}, Sessions: noSessionStore{}, StagedBlobs: noBlobStore{}})
	if err != nil {
		t.Fatal(err)
	}
	identity := sdd.RequestIdentity{Subject: "christopher"}

	show, err := application.Show(t.Context(), identity, "base", sdd.ShowRequest{IDs: []string{"20260713-030000-s-tac-gap", "20260713-050000-s-tac-ins"}})
	if err != nil {
		t.Fatal(err)
	}
	shown, err := presenters.ShowText(show.Items)
	if err != nil {
		t.Fatal(err)
	}
	if len(show.Items) != 2 || show.Entries != shown {
		t.Errorf("Entries should be the shown items rendered\nEntries: %q\nrendered: %q", show.Entries, shown)
	}

	search, err := application.Search(t.Context(), identity, "base", sdd.SearchRequest{SyncMode: sdd.SearchSyncNone, Terms: []string{"kestrel"}})
	if err != nil {
		t.Fatal(err)
	}
	if rendered := presenters.SearchText(search.Hits, search.Notices); len(search.Hits) != 2 || search.Results != rendered {
		t.Errorf("Results should be the hits and notices rendered\nResults: %q\nrendered: %q", search.Results, rendered)
	}

	view, err := application.View(t.Context(), identity, "base", sdd.ViewRequest{Layout: "kind(gap):as-list", Repos: []sdd.RepoID{"example.org/dep"}})
	if err != nil {
		t.Fatal(err)
	}
	if rendered := presenters.ViewText(view.ViewSections); view.Sections != rendered {
		t.Errorf("Sections should be the view sections rendered\nSections: %q\nrendered: %q", view.Sections, rendered)
	}
	if !strings.Contains(view.Sections, "\n── repo: example.org/dep ──\n") || !strings.Contains(view.Sections, "20260713-040000-s-tac-dep") {
		t.Errorf("the dependency's sections should render under its repo heading: %q", view.Sections)
	}
}
