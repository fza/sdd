package application_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const (
	groundedHomeID  = "20260713-110000-s-tac-hom"
	groundedDepID   = "20260713-120000-s-tac-dep"
	groundedDepRepo = "example.test/dep"
)

// groundedDependencyFixture is a home project declaring one writable
// dependency, both enforcing grounding, each holding one entry.
type groundedDependencyFixture struct {
	workflow *sdd.WorkflowSession
	alice    sdd.RequestIdentity
	dirs     map[sdd.ProjectID]string
}

func newGroundedDependencyFixture(t *testing.T) groundedDependencyFixture {
	t.Helper()
	root := t.TempDir()
	dirs := map[sdd.ProjectID]string{"home": filepath.Join(root, "home"), "dep": filepath.Join(root, "dep")}
	writeDependencyEntry(t, dirs["home"], groundedHomeID, "An entry only the home graph holds.")
	writeDependencyEntry(t, dirs["dep"], groundedDepID, "An entry the dependency graph holds.")

	llm := pkgllm.RunnerFunc(func(_ context.Context, request pkgllm.Request) (pkgllm.Result, error) {
		text := `{"findings":[]}`
		switch request.Purpose {
		case pkgllm.PurposeSummarize:
			text = `{"title":"Grounded capture","summary":"A gap captured under grounding."}`
		case pkgllm.PurposeTitle:
			text = `{"title":"Grounded capture"}`
		}
		return pkgllm.Result{Text: text, Identity: pkgllm.Identity{Provider: "test", Model: "test"}}, nil
	})
	runtimes := map[sdd.ProjectID]*sdd.ProjectRuntime{}
	for _, project := range []struct {
		id           sdd.ProjectID
		dependencies []sdd.RepoID
	}{{id: "home", dependencies: []sdd.RepoID{groundedDepRepo}}, {id: "dep"}} {
		graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: project.id, GraphDir: dirs[project.id], Branch: "main"})
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
			Project: sdd.ProjectRef{ID: project.id}, Dependencies: project.dependencies, DefaultBranch: "main",
			Graph: graph, LLM: llm, Grounding: sdd.GroundingPolicy{Enforce: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		runtimes[project.id] = runtime
	}
	sessions, err := localadapter.NewFilesystemSessionStoreAt(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStoreAt(filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	access := &compositionAccess{
		runtimes:     runtimes,
		permissions:  map[string]map[sdd.ProjectID]compositionPermission{"alice": {"home": {read: true, write: true}, "dep": {read: true, write: true}}},
		dependencies: map[sdd.RepoID]sdd.ProjectID{groundedDepRepo: "dep"},
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: access, Sessions: sessions, StagedBlobs: blobs})
	if err != nil {
		t.Fatal(err)
	}
	alice := sdd.RequestIdentity{Subject: "alice"}
	workflow, _, err := application.OpenWorkflow(t.Context(), alice, "home", sdd.WorkflowOpenRequest{ClientName: "alice-mcp"})
	if err != nil {
		t.Fatal(err)
	}
	return groundedDependencyFixture{workflow: workflow, alice: alice, dirs: dirs}
}

// capture runs a gap capture in project through gather, assemble, confirm
// and the summary check, and returns the written entry's ID.
func (f groundedDependencyFixture) capture(t *testing.T, project sdd.ProjectID, gatherID string, refIDs ...string) string {
	t.Helper()
	serve, err := f.workflow.Start(t.Context(), f.alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: project})
	if err != nil {
		t.Fatal(err)
	}
	instance := serve.Instance
	requireRefIDsAdvertised(t, serve.ReportSchema, refIDs)
	if _, err := f.workflow.Gather(t.Context(), f.alice, sdd.WorkflowGatherRequest{Instance: instance, IDs: []string{gatherID}, SyncMode: sdd.SearchSyncNone}); err != nil {
		t.Fatal(err)
	}
	serve = advanceWorkflow(t, f.workflow, f.alice, instance, map[string]any{
		"body": "A gap noticed while reading the graph.", "entryKind": "gap", "layer": "tactical",
		"refs":       refDocuments(refIDs),
		"topics":     []any{"testing/dependency"},
		"confidence": "high",
	})
	if serve.Step != "playback" {
		t.Fatalf("assemble step = %q, want playback; diagnostics %v", serve.Step, serve.Diagnostics)
	}
	serve = advanceWorkflow(t, f.workflow, f.alice, instance, map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"})
	if serve.Step != "verifySummary" {
		t.Fatalf("write step = %q, want verifySummary:\n%s", serve.Step, serve.Instructions)
	}
	serve = advanceWorkflow(t, f.workflow, f.alice, instance, map[string]any{"chooser": "verifySummary", "choice": "faithful", "fields": map[string]any{"fidelityNote": "faithful"}})
	entryID, _ := serve.Produced["entryId"].(string)
	if serve.Status != "completed" || entryID == "" {
		t.Fatalf("capture completion = status %q produced %+v", serve.Status, serve.Produced)
	}
	return entryID
}

// A capture started in a direct dependency writes into the dependency: its
// write step checks for late entries against the dependency's graph, never
// against the home's.
func TestCaptureInDependencyWritesWithoutLateEntries(t *testing.T) {
	fixture := newGroundedDependencyFixture(t)
	entryID := fixture.capture(t, "dep", groundedDepID, groundedDepID)
	assertWorkflowEntryLocation(t, entryID, fixture.dirs["dep"], true)
	assertWorkflowEntryLocation(t, entryID, fixture.dirs["home"], false)
}

// A capture's ref may name an entry of a declared dependency in its
// <repo-id>:<entry-id> form (20260606-003344-d-cpt-s6q); the written entry
// keeps it.
func TestCaptureRefNamesDependencyEntry(t *testing.T) {
	fixture := newGroundedDependencyFixture(t)
	ref := model.CrossRepoID(groundedDepRepo, groundedDepID)
	entryID := fixture.capture(t, "home", groundedHomeID, groundedHomeID, ref)
	path, err := model.IDToRelPath(entryID)
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(fixture.dirs["home"], path))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), ref) {
		t.Fatalf("written entry should keep the ref %s:\n%s", ref, written)
	}
}

func writeDependencyEntry(t *testing.T, dir, id, body string) {
	t.Helper()
	path, err := model.IDToRelPath(id)
	if err != nil {
		t.Fatal(err)
	}
	writeWorkflowBranchEntryAt(t, dir, path, "---\ntype: signal\nkind: gap\nlayer: tactical\nsummary: "+body+"\n---\n\n"+body+"\n")
}

func refDocuments(ids []string) []any {
	refs := make([]any, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, map[string]any{"id": id, "kind": "related", "desc": "an entry read"})
	}
	return refs
}

// requireRefIDsAdvertised fails unless the served report schema's ref ID
// pattern admits every id, so an agent following the schema can send it.
func requireRefIDsAdvertised(t *testing.T, schema map[string]any, ids []string) {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	refs, _ := props["refs"].(map[string]any)
	item, _ := refs["items"].(map[string]any)
	itemProps, _ := item["properties"].(map[string]any)
	id, _ := itemProps["id"].(map[string]any)
	pattern, _ := id["pattern"].(string)
	if pattern == "" {
		t.Fatalf("served schema advertises no ref ID pattern: %v", refs)
	}
	compiled := regexp.MustCompile(pattern)
	for _, ref := range ids {
		if !compiled.MatchString(ref) {
			t.Fatalf("served ref ID pattern %s refuses %s", pattern, ref)
		}
	}
}
