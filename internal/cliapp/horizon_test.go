package cliapp_test

import (
	"strings"
	"testing"
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
