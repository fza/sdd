package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	horizonDepEntry   = "20260901-100000-s-tac-dep"
	horizonDeepEntry  = "20260801-100000-s-tac-dpe"
	horizonStrayEntry = "20260701-100000-s-tac-sty"
	horizonOwnEntry   = "20260915-100000-s-tac-own"

	horizonDeepSummary = "A quetzal summary two steps down."
)

func horizonGap(summary string, refs ...string) string {
	frontmatter := "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: medium\nparticipants: [Local Author]\nsummary: " + summary + "\ntitle: Gap\n"
	if len(refs) > 0 {
		frontmatter += "refs:\n"
	}
	for _, ref := range refs {
		frontmatter += "  - {id: '" + ref + "', kind: related}\n"
	}
	return frontmatter + "---\n\nA zebrafinch gap.\n"
}

// newHorizonRepos builds the chain cli → dep → deep, where the local repo
// names dep alone, and connects stray, which nobody declares. One local entry
// and one dep entry cite deep's entry.
func newHorizonRepos(t *testing.T, f *appFixture) {
	t.Helper()
	newDependencyRepo(t, f, "example.test/deep", nil, map[string]string{"2026/08/01-100000-s-tac-dpe.md": horizonGap(horizonDeepSummary)})
	newDependencyRepo(t, f, "example.test/dep", []string{"example.test/deep"}, map[string]string{
		"2026/09/01-100000-s-tac-dep.md": horizonGap("A gap one step down.", "example.test/deep:"+horizonDeepEntry),
	})
	newDependencyRepo(t, f, "example.test/stray", nil, map[string]string{"2026/07/01-100000-s-tac-sty.md": horizonGap("A stray gap.")})
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/dep]\n")
	f.write(t, "decisions/2026/09/15-100000-s-tac-own.md", horizonGap("A local gap.", "example.test/deep:"+horizonDeepEntry))
}

// TestCLIReadsReachTheDependencyChain: every read beyond the local graph
// reaches dep and deep, which only dep declares, never stray.
func TestCLIReadsReachTheDependencyChain(t *testing.T) {
	f := newAppFixture(t)
	newHorizonRepos(t, f)

	search := f.run(t, "search", "--term", "zebrafinch", "--all-repos")
	if search.err != nil {
		t.Fatalf("search --all-repos: %v\n%s", search.err, search.stderr)
	}
	assertContainsOutput(t, "search --all-repos stdout", search.stdout, "example.test/dep:"+horizonDepEntry, "example.test/deep:"+horizonDeepEntry)
	if strings.Contains(search.stdout, horizonStrayEntry) {
		t.Errorf("search --all-repos reached a repo nobody declares:\n%s", search.stdout)
	}

	named := f.run(t, "search", "--term", "zebrafinch", "--repo", "example.test/deep")
	if named.err != nil || !strings.Contains(named.stdout, "example.test/deep:"+horizonDeepEntry) {
		t.Errorf("search --repo of a second-level dependency: %v\n%s%s", named.err, named.stdout, named.stderr)
	}
	outside := f.run(t, "search", "--term", "zebrafinch", "--repo", "example.test/stray")
	if outside.err == nil || !strings.Contains(outside.err.Error(), "dependency closure") {
		t.Errorf("search --repo outside the chain should be refused naming the dependency closure, got %v", outside.err)
	}

	view := f.run(t, "view", "--all-repos", "--layout", "top(5)")
	if view.err != nil {
		t.Fatalf("view --all-repos: %v\n%s", view.err, view.stderr)
	}
	assertContainsOutput(t, "view --all-repos stdout", view.stdout, "repo: example.test/dep", "repo: example.test/deep")
	if strings.Contains(view.stdout, "example.test/stray") {
		t.Errorf("view --all-repos reached a repo nobody declares:\n%s", view.stdout)
	}

	show := f.run(t, "show", "example.test/deep:"+horizonDeepEntry)
	if show.err != nil {
		t.Errorf("show of a second-level dependency entry: %v\n%s", show.err, show.stderr)
	}
	assertContainsOutput(t, "show of a second-level dependency entry", show.stdout, "id: example.test/deep:"+horizonDeepEntry)
	if show := f.run(t, "show", "example.test/stray:"+horizonStrayEntry); show.err == nil {
		t.Errorf("show reached a repo nobody declares:\n%s", show.stdout)
	}

	own := f.run(t, "show", horizonOwnEntry, "--up", "1", "--down", "0")
	if own.err != nil {
		t.Fatalf("show of the local entry: %v\n%s", own.err, own.stderr)
	}
	assertContainsOutput(t, "show of a local entry citing a second-level dependency", own.stdout, "example.test/deep:"+horizonDeepEntry, horizonDeepSummary)

	viaDep := f.run(t, "show", "example.test/dep:"+horizonDepEntry, "--up", "1", "--down", "0")
	if viaDep.err != nil {
		t.Fatalf("show of the dependency entry: %v\n%s", viaDep.err, viaDep.stderr)
	}
	assertContainsOutput(t, "show of a dependency entry citing its own dependency", viaDep.stdout, horizonDeepSummary)

	export := f.run(t, "export", "--dependencies", "all")
	if export.err != nil {
		t.Fatalf("export: %v\n%s", export.err, export.stderr)
	}
	assertContainsOutput(t, "export stdout", export.stdout, `"example.test/dep"`, `"example.test/deep"`, horizonDeepEntry)
	if strings.Contains(export.stdout, horizonStrayEntry) {
		t.Errorf("export reached a repo nobody declares:\n%s", export.stdout)
	}
}

// TestCLIClonesTheDependencyChain: a read beyond the local graph clones every
// connected repo down the chain into the data root, never one nobody
// declares.
func TestCLIClonesTheDependencyChain(t *testing.T) {
	f := newAppFixture(t)
	newHorizonRepos(t, f)

	if view := f.run(t, "view", "--all-repos", "--layout", "top(5)"); view.err != nil {
		t.Fatalf("view --all-repos: %v\n%s", view.err, view.stderr)
	}
	clones := filepath.Join(dataHome(f.root), "sdd", "repos", "example.test")
	for _, repo := range []string{"dep", "deep"} {
		if _, err := os.Stat(filepath.Join(clones, repo, ".git")); err != nil {
			t.Errorf("%s should be cloned by a read across the chain: %v", repo, err)
		}
	}
	if _, err := os.Stat(filepath.Join(clones, "stray")); err == nil {
		t.Error("a read across the chain cloned a repo nobody declares")
	}
}

// TestRepoSyncReachesEveryConnectedRepo: `sdd repo sync` syncs every repo
// connected on this machine, and a named one whatever declares it.
func TestRepoSyncReachesEveryConnectedRepo(t *testing.T) {
	f := newAppFixture(t)
	newHorizonRepos(t, f)

	sync := f.run(t, "repo", "sync")
	if sync.err != nil {
		t.Fatalf("repo sync: %v\n%s", sync.err, sync.stderr)
	}
	assertContainsOutput(t, "repo sync stdout", sync.stdout, "example.test/dep", "example.test/deep", "example.test/stray")

	named := f.run(t, "repo", "sync", "example.test/deep")
	if named.err != nil || !strings.Contains(named.stdout, "example.test/deep") {
		t.Errorf("repo sync of a second-level dependency: %v\n%s%s", named.err, named.stdout, named.stderr)
	}
}

// TestServeReachesTheDependencyChain runs the same repos through `sdd serve`:
// the session reads across the whole chain and works in deep, never in stray,
// though it is connected and cloned on this machine; nor does a session open
// there.
func TestServeReachesTheDependencyChain(t *testing.T) {
	f := newAppFixture(t)
	newHorizonRepos(t, f)
	if sync := f.run(t, "repo", "sync"); sync.err != nil {
		t.Fatalf("repo sync: %v\n%s", sync.err, sync.stderr)
	}

	f.serve(t, func(client *mcp.ClientSession) {
		call := horizonSession(t, client)

		synced, isError := call("sync_repos", map[string]any{})
		if isError || !strings.Contains(synced, `"repo":"example.test/deep"`) || strings.Contains(synced, `"state":"failed"`) {
			t.Errorf("sync_repos should run the round of every connected repo: %s", synced)
		}

		results, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "all_repos": true})
		if isError {
			t.Fatalf("search all_repos: %s", results)
		}
		for _, want := range []string{"example.test/dep:" + horizonDepEntry, "example.test/deep:" + horizonDeepEntry} {
			if !strings.Contains(results, want) {
				t.Errorf("search all_repos omitted %s:\n%s", want, results)
			}
		}
		if strings.Contains(results, horizonStrayEntry) {
			t.Errorf("search all_repos reached a repo nobody declares:\n%s", results)
		}
		if out, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "repos": []string{"example.test/stray"}}); !isError {
			t.Errorf("search of a repo nobody declares succeeded:\n%s", out)
		}
		if out, isError := call("view", map[string]any{"layout": "kind(gap):as-list", "repos": []string{"example.test/deep"}}); isError || !strings.Contains(out, horizonDeepEntry) {
			t.Errorf("view over a second-level dependency: %s", out)
		}
		if out, isError := call("show", map[string]any{"ids": []string{"example.test/deep:" + horizonDeepEntry}, "up": 0, "down": 0}); isError || !strings.Contains(out, "id: example.test/deep:"+horizonDeepEntry) {
			t.Errorf("show of a second-level dependency entry from home: %s", out)
		}
		if out, isError := call("show", map[string]any{"ids": []string{horizonOwnEntry}, "up": 1, "down": 0}); isError || !strings.Contains(out, horizonDeepSummary) {
			t.Errorf("show of a local entry citing a second-level dependency should resolve the ref: %s", out)
		}
		if out, isError := call("show", map[string]any{"ids": []string{horizonDeepEntry}, "project": "example.test/deep", "up": 0, "down": 0}); isError || !strings.Contains(out, "id: "+horizonDeepEntry) {
			t.Errorf("show in a second-level dependency project: %s", out)
		}
		if out, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "project": "example.test/deep"}); isError || !strings.Contains(out, horizonDeepEntry) {
			t.Errorf("search in a second-level dependency project: %s", out)
		}
		if out, isError := call("info", map[string]any{"project": "example.test/deep"}); isError {
			t.Errorf("info of a second-level dependency project: %s", out)
		}
		if out, isError := call("show", map[string]any{"ids": []string{horizonStrayEntry}, "project": "example.test/stray", "up": 0, "down": 0}); !isError || !strings.Contains(out, "dependency closure") {
			t.Errorf("show in a project nobody declares should be refused naming the dependency closure:\n%s", out)
		}
		if out, isError := call("start_procedure", map[string]any{"canonical": "capture", "project": "example.test/stray"}); !isError || !strings.Contains(out, "dependency closure") {
			t.Errorf("a move into a project nobody declares should be refused naming the dependency closure:\n%s", out)
		}
		if out, isError := call("start_procedure", map[string]any{"canonical": "capture", "project": "example.test/deep"}); isError && (strings.Contains(out, "dependency closure") || strings.Contains(out, "direct dependency")) {
			t.Errorf("a move into a second-level dependency should pass the dependency check:\n%s", out)
		}

		for project, wantOpen := range map[string]bool{"example.test/dep": true, "example.test/deep": true, "example.test/stray": false} {
			opened, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{"project": project}})
			if err != nil {
				t.Fatalf("start_session in %s: %v", project, err)
			}
			if opened.IsError == wantOpen {
				t.Errorf("start_session in %s: opened = %v, want %v", project, !opened.IsError, wantOpen)
			}
		}
	})
}

// TestUnconnectedSecondLevelMember: gone, which only dep declares, is not
// connected on this machine. The CLI reads around it and marks a ref into it
// as unresolved; the server fails only a read that selects it
// (20261028-155600-s-tac-4mz).
func TestUnconnectedSecondLevelMember(t *testing.T) {
	f := newAppFixture(t)
	newDependencyRepo(t, f, "example.test/dep", []string{"example.test/gone"}, map[string]string{
		"2026/09/01-100000-s-tac-dep.md": horizonGap("A gap one step down.", "example.test/gone:20260804-100000-s-tac-gon"),
	})
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/dep]\n")

	search := f.run(t, "search", "--term", "zebrafinch", "--all-repos")
	if search.err != nil || !strings.Contains(search.stdout, "example.test/dep:"+horizonDepEntry) {
		t.Errorf("search --all-repos should read dep around the unconnected second-level member: %v\n%s%s", search.err, search.stdout, search.stderr)
	}
	show := f.run(t, "show", "example.test/dep:"+horizonDepEntry, "--up", "1", "--down", "0")
	if show.err != nil {
		t.Fatalf("show of an entry citing a repo not connected: %v\n%s", show.err, show.stderr)
	}
	assertContainsOutput(t, "show of a ref into a repo not connected", show.stdout, "[unresolved: repo example.test/gone]")

	f.serve(t, func(client *mcp.ClientSession) {
		call := horizonSession(t, client)
		if out, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "repos": []string{"example.test/dep"}}); isError || !strings.Contains(out, "example.test/dep:"+horizonDepEntry) {
			t.Errorf("search naming dep should read it beside the unconnected second-level member:\n%s", out)
		}
		if out, isError := call("show", map[string]any{"ids": []string{"example.test/dep:" + horizonDepEntry}, "up": 1, "down": 0}); isError || !strings.Contains(out, "[unresolved: repo example.test/gone]") {
			t.Errorf("show should mark a ref into a repo not connected:\n%s", out)
		}
		if out, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "all_repos": true}); !isError || !strings.Contains(out, "dependency unavailable") {
			t.Errorf("search all_repos selects the unconnected member and should fail naming it unavailable:\n%s", out)
		}
	})
}

// horizonSession opens a session and returns a caller of its tools that
// reports the text content and whether the tool failed.
func horizonSession(t *testing.T, client *mcp.ClientSession) func(tool string, args map[string]any) (string, bool) {
	t.Helper()
	door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
	if err != nil || door.IsError {
		t.Fatalf("start_session: %v %+v", err, door)
	}
	handle, _ := door.StructuredContent.(map[string]any)["session"].(string)
	return func(tool string, args map[string]any) (string, bool) {
		t.Helper()
		args["session"] = handle
		result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: answered(tool, args)})
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		var text strings.Builder
		for _, content := range result.Content {
			if c, ok := content.(*mcp.TextContent); ok {
				text.WriteString(c.Text)
			}
		}
		return text.String(), result.IsError
	}
}

// TestCLIReadProceedsWhenRefreshFails: a dependency whose remote is
// unreachable is reported as a warning, and the read proceeds on the caches
// as they stand (20261027-150208-d-tac-bg4).
func TestCLIReadProceedsWhenRefreshFails(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nrepos:\n  - repo_id: example.test/gone\n    clone_url: "+f.root+"/no-such-remote\n")
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/gone]\n")

	result := f.run(t, "export", "--dependencies", "all")
	if result.err != nil {
		t.Fatalf("export failed on an unreachable dependency: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "export stderr", result.stderr, "could not refresh dependency", "example.test/gone")
	assertContainsOutput(t, "export stdout", result.stdout, `"example.test/gone"`)
}
