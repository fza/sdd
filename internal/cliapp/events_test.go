package cliapp_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventsImportExport_RoundTripsThroughTheBinaryLog(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nevents:\n  channels:\n    recheck:\n      share: git\n")
	lines := `{"id":"e1","channel":"recheck","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["20261109-004506-d-cpt-udz","20261107-115150-d-cpt-0mu"]}}
{"id":"e2","channel":"wip","type":"sdd.wip","time":"2026-11-09T12:01:00.5Z","writer":"Local Author","subject":{"uri":"https://tracker.example/issues/7"},"payload":{"step":"start"}}
`
	f.write(t, "in.jsonl", lines)

	imported := f.run(t, "events", "import", "in.jsonl")
	if imported.err != nil {
		t.Fatalf("import: %v\n%s", imported.err, imported.stderr)
	}
	if !strings.Contains(imported.stdout, "Imported 2 events") {
		t.Fatalf("import output = %q", imported.stdout)
	}
	segments, err := filepath.Glob(filepath.Join(f.root, ".sdd", "events", "*", "*.seg"))
	if err != nil || len(segments) != 2 {
		t.Fatalf("import should write one segment per channel under .sdd/events, found %v (%v)", segments, err)
	}
	_, err = os.Stat(filepath.Join(f.root, ".sdd", "events", "recheck", ".gitignore"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the committed config shares recheck through Git, yet it ignores itself: %v", err)
	}
	_, err = os.Stat(filepath.Join(f.root, ".sdd", "events", "wip", ".gitignore"))
	if err != nil {
		t.Fatalf("the unconfigured wip channel should stay local: %v", err)
	}

	exported := f.run(t, "events", "export")
	if exported.err != nil {
		t.Fatalf("export: %v\n%s", exported.err, exported.stderr)
	}
	if exported.stdout != lines {
		t.Fatalf("export differs from the imported lines:\n%s\nwant\n%s", exported.stdout, lines)
	}
	onlyWip := f.run(t, "events", "export", "--channel", "wip")
	if onlyWip.err != nil || strings.Contains(onlyWip.stdout, `"e1"`) || !strings.Contains(onlyWip.stdout, `"e2"`) {
		t.Fatalf("export --channel wip = %q, %v", onlyWip.stdout, onlyWip.err)
	}

	again := f.run(t, "events", "import", "in.jsonl")
	if again.err != nil || !strings.Contains(again.stdout, "Imported 0 events") {
		t.Fatalf("importing stored events again = %q, %v", again.stdout, again.err)
	}
}

func TestEventsImport_NeedsOneSource(t *testing.T) {
	f := newAppFixture(t)
	for _, args := range [][]string{{"events", "import"}, {"events", "import", "a.jsonl", "b.jsonl"}} {
		result := f.run(t, args...)
		if result.err == nil || !strings.Contains(result.err.Error(), "exactly one file") {
			t.Fatalf("%v = %v; want a refusal asking for exactly one file", args, result.err)
		}
	}
}

func TestEvents_RefuseAnUnknownSharingMode(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nevents:\n  channels:\n    recheck:\n      share: hosted\n")
	result := f.run(t, "events", "export")
	if result.err == nil || !strings.Contains(result.err.Error(), "events.channels.recheck.share") {
		t.Fatalf("export under an unknown share = %v; want an error naming the key", result.err)
	}
}

func TestEventsCompact_MergesEachChannel(t *testing.T) {
	f := newAppFixture(t)
	for i, line := range []string{
		`{"id":"e1","channel":"wip","type":"sdd.wip","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"uri":"https://tracker.example/issues/7"}}`,
		`{"id":"e2","channel":"wip","type":"sdd.wip","time":"2026-11-09T12:01:00Z","writer":"sdd","subject":{"uri":"https://tracker.example/issues/7"}}`,
	} {
		name := filepath.Join("in", string(rune('a'+i))+".jsonl")
		f.write(t, name, line+"\n")
		if imported := f.run(t, "events", "import", name); imported.err != nil {
			t.Fatal(imported.err)
		}
	}
	compacted := f.run(t, "events", "compact")
	if compacted.err != nil || !strings.Contains(compacted.stdout, "Compacted") {
		t.Fatalf("compact = %q, %v", compacted.stdout, compacted.err)
	}
	segments, err := filepath.Glob(filepath.Join(f.root, ".sdd", "events", "wip", "*.seg"))
	if err != nil || len(segments) != 1 {
		t.Fatalf("compact should leave one segment in the channel, found %v (%v)", segments, err)
	}
	exported := f.run(t, "events", "export")
	if exported.err != nil || !strings.Contains(exported.stdout, `"e1"`) || !strings.Contains(exported.stdout, `"e2"`) {
		t.Fatalf("export after compact = %q, %v", exported.stdout, exported.err)
	}
}

// Compaction drops the recheck items a later entry healed and keeps the
// open ones (D17).
func TestEventsCompact_DropsHealedRecheckItems(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n")
	entry := func(id, refs string) {
		f.write(t, "decisions/"+id[:4]+"/"+id[4:6]+"/"+id[6:]+".md",
			"---\ntype: decision\nkind: directive\nlayer: tactical\nintent: pending\nconfidence: high\n"+refs+"summary: Entry "+id+".\n---\n\nEntry "+id+".\n")
	}
	entry("20261101-120000-d-tac-aaa", "")
	entry("20261102-120000-d-tac-bbb", "")
	entry("20261103-120000-d-tac-ccc", "")
	entry("20261105-120000-d-tac-hea", "refs:\n  - id: 20261101-120000-d-tac-aaa\n    kind: addresses\n  - id: 20261102-120000-d-tac-bbb\n    kind: addresses\n")
	f.write(t, "in.jsonl", `{"id":"recheck-healed","channel":"recheck","type":"sdd.recheck","time":"2026-11-04T00:00:00Z","writer":"sdd","subject":{"entries":["20261101-120000-d-tac-aaa","20261102-120000-d-tac-bbb"]},"payload":{"kind":"strain","relation":"contradicts"}}
{"id":"recheck-open","channel":"recheck","type":"sdd.recheck","time":"2026-11-04T00:00:00Z","writer":"sdd","subject":{"entries":["20261101-120000-d-tac-aaa","20261103-120000-d-tac-ccc"]},"payload":{"kind":"strain","relation":"outdates"}}
`)
	if imported := f.run(t, "events", "import", "in.jsonl"); imported.err != nil {
		t.Fatal(imported.err)
	}
	if compacted := f.run(t, "events", "compact"); compacted.err != nil {
		t.Fatal(compacted.err)
	}
	exported := f.run(t, "events", "export")
	if exported.err != nil || strings.Contains(exported.stdout, "recheck-healed") || !strings.Contains(exported.stdout, "recheck-open") {
		t.Fatalf("export after compact = %q, %v: the healed item should go, the open one stay", exported.stdout, exported.err)
	}
}
