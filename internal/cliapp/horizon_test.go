package cliapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCLIReadsReachTheDependencyClosure: the local repo declares dep, dep
// declares deep, and stray is connected on this machine but declared by
// nobody. Every read beyond the local graph reaches dep and deep, never stray
// (20261026-194528-d-cpt-gcq).
func TestCLIReadsReachTheDependencyClosure(t *testing.T) {
	f := newAppFixture(t)
	const (
		depEntry   = "20260901-100000-s-tac-dep"
		deepEntry  = "20260801-100000-s-tac-dpe"
		strayEntry = "20260701-100000-s-tac-sty"
	)
	gap := "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: medium\nparticipants: [Local Author]\n---\n\nA zebrafinch gap.\n"
	newDependencyRepo(t, f, "example.test/deep", nil, map[string]string{"2026/08/01-100000-s-tac-dpe.md": gap})
	newDependencyRepo(t, f, "example.test/dep", []string{"example.test/deep"}, map[string]string{"2026/09/01-100000-s-tac-dep.md": gap})
	newDependencyRepo(t, f, "example.test/stray", nil, map[string]string{"2026/07/01-100000-s-tac-sty.md": gap})
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/dep]\n")

	search := f.run(t, "search", "--term", "zebrafinch", "--all-repos")
	if search.err != nil {
		t.Fatalf("search --all-repos: %v\n%s", search.err, search.stderr)
	}
	assertContainsOutput(t, "search --all-repos stdout", search.stdout, "example.test/dep:"+depEntry, "example.test/deep:"+deepEntry)
	if strings.Contains(search.stdout, strayEntry) {
		t.Errorf("search --all-repos reached an undeclared repo:\n%s", search.stdout)
	}

	named := f.run(t, "search", "--term", "zebrafinch", "--repo", "example.test/stray")
	if named.err == nil || !strings.Contains(named.err.Error(), "dependency closure") {
		t.Errorf("search --repo outside the closure: err = %v, want a closure refusal", named.err)
	}

	view := f.run(t, "view", "--all-repos", "--layout", "top(5)")
	if view.err != nil {
		t.Fatalf("view --all-repos: %v\n%s", view.err, view.stderr)
	}
	assertContainsOutput(t, "view --all-repos stdout", view.stdout, "repo: example.test/dep", "repo: example.test/deep")
	if strings.Contains(view.stdout, "example.test/stray") {
		t.Errorf("view --all-repos reached an undeclared repo:\n%s", view.stdout)
	}

	if show := f.run(t, "show", "example.test/deep:"+deepEntry); show.err != nil {
		t.Errorf("show of a second-level dependency entry: %v\n%s", show.err, show.stderr)
	}
	if show := f.run(t, "show", "example.test/stray:"+strayEntry); show.err == nil {
		t.Errorf("show reached an undeclared repo:\n%s", show.stdout)
	}
}

// TestServeReachesTheDependencyClosure runs the same repos through `sdd
// serve`: the session reads across the whole closure, works in a
// second-level dependency, and never reaches stray, though it is connected and
// cached on this machine.
func TestServeReachesTheDependencyClosure(t *testing.T) {
	f := newAppFixture(t)
	const (
		depEntry   = "20260901-100000-s-tac-dep"
		deepEntry  = "20260801-100000-s-tac-dpe"
		strayEntry = "20260701-100000-s-tac-sty"
	)
	gap := "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: medium\nparticipants: [Local Author]\n---\n\nA zebrafinch gap.\n"
	newDependencyRepo(t, f, "example.test/deep", nil, map[string]string{"2026/08/01-100000-s-tac-dpe.md": gap})
	newDependencyRepo(t, f, "example.test/dep", []string{"example.test/deep"}, map[string]string{"2026/09/01-100000-s-tac-dep.md": gap})
	newDependencyRepo(t, f, "example.test/stray", nil, map[string]string{"2026/07/01-100000-s-tac-sty.md": gap})
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/dep]\n")
	if sync := f.run(t, "repo", "sync"); sync.err != nil {
		t.Fatalf("repo sync: %v\n%s", sync.err, sync.stderr)
	}

	f.serve(t, func(client *mcp.ClientSession) {
		door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
		if err != nil || door.IsError {
			t.Fatalf("start_session: %v %+v", err, door)
		}
		handle, _ := door.StructuredContent.(map[string]any)["session"].(string)
		call := func(tool string, args map[string]any) (string, bool) {
			t.Helper()
			args["session"] = handle
			result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
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

		results, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "all_repos": true})
		if isError {
			t.Fatalf("search all_repos: %s", results)
		}
		for _, want := range []string{"example.test/dep:" + depEntry, "example.test/deep:" + deepEntry} {
			if !strings.Contains(results, want) {
				t.Errorf("search all_repos omitted %s:\n%s", want, results)
			}
		}
		if strings.Contains(results, strayEntry) {
			t.Errorf("search all_repos reached an undeclared repo:\n%s", results)
		}
		if out, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "repos": []string{"example.test/stray"}}); !isError {
			t.Errorf("search of an undeclared repo succeeded:\n%s", out)
		}
		if out, isError := call("show", map[string]any{"ids": []string{"example.test/deep:" + deepEntry}, "up": 0, "down": 0}); isError || !strings.Contains(out, deepEntry) {
			t.Errorf("show of a second-level dependency entry from home: %s", out)
		}
		if out, isError := call("show", map[string]any{"ids": []string{deepEntry}, "project": "example.test/deep", "up": 0, "down": 0}); isError || !strings.Contains(out, deepEntry) {
			t.Errorf("show in a second-level dependency project: %s", out)
		}
		if out, isError := call("show", map[string]any{"ids": []string{strayEntry}, "project": "example.test/stray", "up": 0, "down": 0}); !isError {
			t.Errorf("show in an undeclared project succeeded:\n%s", out)
		}
	})
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
