package cliapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const horizonGap = "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: medium\nparticipants: [Local Author]\n---\n\nA zebrafinch gap.\n"

const (
	horizonDepEntry   = "20260901-100000-s-tac-dep"
	horizonDeepEntry  = "20260801-100000-s-tac-dpe"
	horizonStrayEntry = "20260701-100000-s-tac-sty"
)

// newHorizonRepos connects dep, which the local repo declares, deep, which
// only dep declares, and stray, which nobody declares.
func newHorizonRepos(t *testing.T, f *appFixture) {
	t.Helper()
	newDependencyRepo(t, f, "example.test/deep", nil, map[string]string{"2026/08/01-100000-s-tac-dpe.md": horizonGap})
	newDependencyRepo(t, f, "example.test/dep", []string{"example.test/deep"}, map[string]string{"2026/09/01-100000-s-tac-dep.md": horizonGap})
	newDependencyRepo(t, f, "example.test/stray", nil, map[string]string{"2026/07/01-100000-s-tac-sty.md": horizonGap})
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/dep]\n")
}

// TestCLIReadsReachDirectDependenciesOnly: every read beyond the local graph
// reaches dep, never deep nor stray (20261113-235859-d-cpt-l3m).
func TestCLIReadsReachDirectDependenciesOnly(t *testing.T) {
	f := newAppFixture(t)
	newHorizonRepos(t, f)

	search := f.run(t, "search", "--term", "zebrafinch", "--all-repos")
	if search.err != nil {
		t.Fatalf("search --all-repos: %v\n%s", search.err, search.stderr)
	}
	assertContainsOutput(t, "search --all-repos stdout", search.stdout, "example.test/dep:"+horizonDepEntry)
	for _, outside := range []string{horizonDeepEntry, horizonStrayEntry} {
		if strings.Contains(search.stdout, outside) {
			t.Errorf("search --all-repos reached %s beyond the direct dependencies:\n%s", outside, search.stdout)
		}
	}

	named := f.run(t, "search", "--term", "zebrafinch", "--repo", "example.test/deep")
	if named.err == nil || !strings.Contains(named.err.Error(), "not a direct dependency of example.test/cli") {
		t.Errorf("search --repo of a second-level dependency should be refused as no direct dependency, got %v", named.err)
	}

	view := f.run(t, "view", "--all-repos", "--layout", "top(5)")
	if view.err != nil {
		t.Fatalf("view --all-repos: %v\n%s", view.err, view.stderr)
	}
	assertContainsOutput(t, "view --all-repos stdout", view.stdout, "repo: example.test/dep")
	if strings.Contains(view.stdout, "example.test/deep") || strings.Contains(view.stdout, "example.test/stray") {
		t.Errorf("view --all-repos reached beyond the direct dependencies:\n%s", view.stdout)
	}

	if show := f.run(t, "show", "example.test/dep:"+horizonDepEntry); show.err != nil {
		t.Errorf("show of a direct dependency entry: %v\n%s", show.err, show.stderr)
	}
	for _, id := range []string{"example.test/deep:" + horizonDeepEntry, "example.test/stray:" + horizonStrayEntry} {
		if show := f.run(t, "show", id); show.err == nil {
			t.Errorf("show reached %s beyond the direct dependencies:\n%s", id, show.stdout)
		}
	}
}

// TestRepoSyncReachesDirectDependenciesOnly: `sdd repo sync` in a project
// syncs its direct dependencies and refuses a named repo outside them.
func TestRepoSyncReachesDirectDependenciesOnly(t *testing.T) {
	f := newAppFixture(t)
	newHorizonRepos(t, f)

	sync := f.run(t, "repo", "sync")
	if sync.err != nil {
		t.Fatalf("repo sync: %v\n%s", sync.err, sync.stderr)
	}
	assertContainsOutput(t, "repo sync stdout", sync.stdout, "example.test/dep")
	if strings.Contains(sync.stdout, "example.test/deep") || strings.Contains(sync.stdout, "example.test/stray") {
		t.Errorf("repo sync reached beyond the direct dependencies:\n%s", sync.stdout)
	}

	named := f.run(t, "repo", "sync", "example.test/deep")
	if named.err == nil || !strings.Contains(named.stdout, "not a direct dependency of example.test/cli") {
		t.Errorf("repo sync of a second-level dependency should be refused as no direct dependency, got %v\n%s", named.err, named.stdout)
	}
}

// TestServeReachesDirectDependenciesOnly runs the same repos through `sdd
// serve`: the session reads and works in dep, never in deep nor stray, though
// both are connected and cloned on this machine; nor does a session open
// there.
func TestServeReachesDirectDependenciesOnly(t *testing.T) {
	f := newAppFixture(t)
	newHorizonRepos(t, f)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/dep, example.test/deep, example.test/stray]\n")
	if sync := f.run(t, "repo", "sync"); sync.err != nil {
		t.Fatalf("repo sync: %v\n%s", sync.err, sync.stderr)
	}
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/dep]\n")

	f.serve(t, func(client *mcp.ClientSession) {
		call := horizonSession(t, client)

		synced, isError := call("sync_repos", map[string]any{})
		if isError || !strings.Contains(synced, `"repo":"example.test/dep"`) || strings.Contains(synced, "example.test/deep") {
			t.Errorf("sync_repos should run the round of the direct dependencies only: %s", synced)
		}

		results, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "all_repos": true})
		if isError {
			t.Fatalf("search all_repos: %s", results)
		}
		if !strings.Contains(results, "example.test/dep:"+horizonDepEntry) {
			t.Errorf("search all_repos omitted the direct dependency:\n%s", results)
		}
		if strings.Contains(results, horizonDeepEntry) || strings.Contains(results, horizonStrayEntry) {
			t.Errorf("search all_repos reached beyond the direct dependencies:\n%s", results)
		}
		if out, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "repos": []string{"example.test/deep"}}); !isError || !strings.Contains(out, "not a direct dependency") {
			t.Errorf("search of a second-level dependency should be refused as no direct dependency:\n%s", out)
		}
		if out, isError := call("show", map[string]any{"ids": []string{horizonDepEntry}, "project": "example.test/dep", "up": 0, "down": 0}); isError || !strings.Contains(out, horizonDepEntry) {
			t.Errorf("show in a direct dependency project: %s", out)
		}
		for _, project := range []string{"example.test/deep", "example.test/stray"} {
			if out, isError := call("show", map[string]any{"ids": []string{horizonDeepEntry}, "project": project, "up": 0, "down": 0}); !isError || !strings.Contains(out, "not a direct dependency of example.test/cli") {
				t.Errorf("show in %s should be refused as no direct dependency:\n%s", project, out)
			}
		}
		if out, isError := call("start_procedure", map[string]any{"canonical": "capture", "project": "example.test/deep"}); !isError || !strings.Contains(out, "not a direct dependency of example.test/cli") {
			t.Errorf("a move into a second-level dependency should be refused as no direct dependency:\n%s", out)
		}

		for project, wantOpen := range map[string]bool{"example.test/dep": true, "example.test/deep": false, "example.test/stray": false} {
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

// TestServeAllReposIgnoresUnreachableSecondLevel reproduces
// 20261028-155600-s-tac-4mz: deep, which only dep declares, is neither
// connected nor cloned on this machine. An all-repos read from the home
// never selects it, so it no longer fails.
func TestServeAllReposIgnoresUnreachableSecondLevel(t *testing.T) {
	f := newAppFixture(t)
	newDependencyRepo(t, f, "example.test/dep", []string{"example.test/deep"}, map[string]string{"2026/09/01-100000-s-tac-dep.md": horizonGap})
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/dep]\n")
	if sync := f.run(t, "repo", "sync"); sync.err != nil {
		t.Fatalf("repo sync: %v\n%s", sync.err, sync.stderr)
	}

	f.serve(t, func(client *mcp.ClientSession) {
		call := horizonSession(t, client)
		results, isError := call("search", map[string]any{"terms": []string{"zebrafinch"}, "all_repos": true})
		if isError || !strings.Contains(results, "example.test/dep:"+horizonDepEntry) {
			t.Errorf("search all_repos should read dep around the unreachable second-level member:\n%s", results)
		}
		view, isError := call("view", map[string]any{"all_repos": true, "layout": "top(5)"})
		if isError || !strings.Contains(view, "example.test/dep") {
			t.Errorf("view all_repos should read dep around the unreachable second-level member:\n%s", view)
		}
	})
	search := f.run(t, "search", "--term", "zebrafinch", "--all-repos")
	if search.err != nil || !strings.Contains(search.stdout, "example.test/dep:"+horizonDepEntry) {
		t.Errorf("CLI search --all-repos should read dep around the unreachable second-level member: %v\n%s%s", search.err, search.stdout, search.stderr)
	}
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
