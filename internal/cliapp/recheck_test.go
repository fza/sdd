package cliapp_test

import (
	"strings"
	"testing"
)

func TestRecheck_ListsOpenItemsAndScans(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n")
	f.write(t, "decisions/2026/11/01-120000-d-tac-aaa.md", "---\ntype: decision\nkind: directive\nlayer: tactical\nintent: pending\nconfidence: high\nsummary: The edited entry.\n---\n\nThe edited entry.\n")
	empty := f.run(t, "recheck", "list")
	if empty.err != nil || !strings.Contains(empty.stdout, "No open recheck items") {
		t.Fatalf("list = %q, %v", empty.stdout, empty.err)
	}
	scanned := f.run(t, "recheck", "scan")
	if scanned.err != nil || !strings.Contains(scanned.stdout, "Found 0 hand edits") {
		t.Fatalf("scan = %q, %v", scanned.stdout, scanned.err)
	}
	f.write(t, "in.jsonl", `{"id":"recheck-edit","channel":"recheck","type":"sdd.recheck","time":"2026-11-04T00:00:00Z","writer":"sdd","subject":{"entries":["20261101-120000-d-tac-aaa"]},"payload":{"kind":"hand-edit","commit":"0123456789abcdef"}}
`)
	if imported := f.run(t, "events", "import", "in.jsonl"); imported.err != nil {
		t.Fatal(imported.err)
	}
	listed := f.run(t, "recheck", "list")
	if listed.err != nil || !strings.Contains(listed.stdout, "hand edit in commit 0123456789ab: 20261101-120000-d-tac-aaa The edited entry.") {
		t.Fatalf("list = %q, %v", listed.stdout, listed.err)
	}
}
