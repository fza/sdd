package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	furtherPresent = "20260801-100000-s-tac-pre"
	furtherLanded  = "20260802-100000-s-tac-lan"
	furtherAbsent  = "20260803-100000-s-tac-abs"
	furtherNowhere = "20260804-100000-s-tac-now"
)

func furtherGap(summary string) string {
	return "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: medium\nparticipants: [Local Author]\nsummary: " + summary + "\n---\n\n" + summary + "\n"
}

func refEntry(refs ...string) string {
	frontmatter := "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: medium\nparticipants: [Local Author]\nrefs:\n"
	for _, ref := range refs {
		frontmatter += "  - {id: '" + ref + "', kind: related}\n"
	}
	return frontmatter + "---\n\nA gap citing further out.\n"
}

// newFurtherRepos connects deep, which only dep declares, and dep, which the
// local repo declares; each dep entry refs one entry of deep or of nowhere,
// which no machine connects (20261113-235859-d-cpt-l3m).
func newFurtherRepos(t *testing.T, f *appFixture) (deepSource string) {
	t.Helper()
	deepSource = newDependencyRepo(t, f, "example.test/deep", nil, map[string]string{"2026/08/01-100000-s-tac-pre.md": furtherGap("Zanzibar present summary.")})
	newDependencyRepo(t, f, "example.test/dep", []string{"example.test/deep", "example.test/nowhere"}, map[string]string{
		"2026/09/01-100000-s-tac-one.md": refEntry("example.test/deep:" + furtherPresent),
		"2026/09/02-100000-s-tac-two.md": refEntry("example.test/deep:" + furtherLanded),
		"2026/09/03-100000-s-tac-thr.md": refEntry("example.test/deep:" + furtherAbsent),
		"2026/09/04-100000-s-tac-fou.md": refEntry("example.test/nowhere:" + furtherNowhere),
	})
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\ndependencies: [example.test/dep]\n")
	return deepSource
}

func TestCLIResolvesRefsFurtherOut(t *testing.T) {
	f := newAppFixture(t)
	deepSource := newFurtherRepos(t, f)
	deepClone := filepath.Join(dataHome(f.root), "sdd", "repos", "example.test", "deep")

	show := f.run(t, "show", "example.test/dep:20260901-100000-s-tac-one", "--up", "1", "--down", "0")
	if show.err != nil {
		t.Fatalf("show: %v\n%s", show.err, show.stderr)
	}
	assertContainsOutput(t, "show of a ref into an uncloned connected repo", show.stdout, "example.test/deep:"+furtherPresent, "Zanzibar present summary", "[clone as of ")
	if _, err := os.Stat(filepath.Join(deepClone, ".git")); err != nil {
		t.Fatalf("a connected repo without a clone should be cloned on the miss: %v", err)
	}

	writeSourceEntry(t, deepSource, "2026/08/02-100000-s-tac-lan.md", furtherGap("Zanzibar landed summary."))
	show = f.run(t, "show", "example.test/dep:20260901-100000-s-tac-one", "--up", "1", "--down", "0")
	assertContainsOutput(t, "show from the existing clone", show.stdout, "Zanzibar present summary", "[clone as of ")
	if _, err := os.Stat(filepath.Join(deepClone, ".sdd", "graph", "2026", "08", "02-100000-s-tac-lan.md")); err == nil {
		t.Error("a ref the clone holds should resolve without syncing the clone")
	}

	show = f.run(t, "show", "example.test/dep:20260902-100000-s-tac-two", "--up", "1", "--down", "0")
	assertContainsOutput(t, "show of a ref the clone lacks", show.stdout, "Zanzibar landed summary")

	show = f.run(t, "show", "example.test/dep:20260903-100000-s-tac-thr", "--up", "1", "--down", "0")
	assertContainsOutput(t, "show of a ref missing after the fetch", show.stdout, "[unresolved: repo example.test/deep: not in example.test/deep as of ")
	assertContainsOutput(t, "show stderr", show.stderr, "missing-after-fetch", "example.test/deep:"+furtherAbsent)
	if _, err := os.Stat(filepath.Join(f.root, "state", "sdd", "ref-misses.json")); err != nil {
		t.Errorf("the miss should be recorded beside the gate's record: %v", err)
	}

	show = f.run(t, "show", "example.test/dep:20260904-100000-s-tac-fou", "--up", "1", "--down", "0")
	assertContainsOutput(t, "show of a ref into a repo not connected", show.stdout, "[unresolved: repo example.test/nowhere: not connected on this machine]")
	assertContainsOutput(t, "show stderr", show.stderr, "not-connected", "sdd repo add")

	view := f.run(t, "view", "--repo", "example.test/dep", "--layout", "kind(gap):expand(refs):as-list")
	assertContainsOutput(t, "view of the dependency", view.stdout, "[unresolved: repo example.test/nowhere: not connected on this machine]", "[clone as of ")
}

func TestServeResolvesRefsFurtherOut(t *testing.T) {
	f := newAppFixture(t)
	newFurtherRepos(t, f)
	if sync := f.run(t, "repo", "sync"); sync.err != nil {
		t.Fatalf("repo sync: %v\n%s", sync.err, sync.stderr)
	}
	f.serve(t, func(client *mcp.ClientSession) {
		call := horizonSession(t, client)
		out, isError := call("show", map[string]any{"ids": []string{"example.test/dep:20260901-100000-s-tac-one"}, "up": 1, "down": 0})
		if isError || !strings.Contains(out, "Zanzibar present summary") || !strings.Contains(out, "[clone as of ") {
			t.Errorf("show should resolve a ref further out from its clone:\n%s", out)
		}
		out, isError = call("show", map[string]any{"ids": []string{"example.test/dep:20260904-100000-s-tac-fou"}, "up": 1, "down": 0})
		if isError || !strings.Contains(out, "[unresolved: repo example.test/nowhere: not connected on this machine]") {
			t.Errorf("show should mark a ref into a repo not connected:\n%s", out)
		}
		if !strings.Contains(out, "not-connected") || !strings.Contains(out, "sdd repo add") {
			t.Errorf("the result should carry the typed warning with who must act:\n%s", out)
		}
	})
}

// writeSourceEntry commits a graph entry to a dependency's source repo.
func writeSourceEntry(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, ".sdd", "graph", filepath.FromSlash(rel))
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "land an entry")
}
