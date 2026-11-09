package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
)

const relevanceLines = `{"type":"reader","a":"20261101-120000-s-tac-aaa","b":"20261109-004506-d-cpt-udz","code":"dismissed","reason":"term-clash"}
{"type":"score","a":"20261101-120000-s-tac-aaa","b":"20261109-004506-d-cpt-udz","judge":"systemone:jev-1.14.0","direction":"both","score":17}
`

func TestRelevance_LivesWhereTheCommittedConfigPutsIt(t *testing.T) {
	cases := []struct {
		name      string
		config    string
		committed string
		state     string
	}{
		{"machine state by default", "", "", "ledger"},
		{"committed with every record", "relevance:\n  commit: true\n", "ledger", ""},
		{"committed reader verdicts, local scores", "relevance:\n  commit: true\n  content: reader\n", "reader", "scores"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n"+tc.config)
			f.write(t, "in.jsonl", relevanceLines)
			imported := f.run(t, "relevance", "import", "in.jsonl")
			if imported.err != nil || !strings.Contains(imported.stdout, "Imported 2 records") {
				t.Fatalf("import = %q, %v\n%s", imported.stdout, imported.err, imported.stderr)
			}
			committed := ledgerRecords(t, filepath.Join(f.root, ".sdd", "relevance"))
			state := ledgerRecords(t, filepath.Join(f.root, "state", "sdd", "relevance", "example.test", "cli"))
			if committed != tc.committed || state != tc.state {
				t.Fatalf("committed holds %q, machine state %q", committed, state)
			}
			exported := f.run(t, "relevance", "export")
			if exported.err != nil || exported.stdout != relevanceLines {
				t.Fatalf("export = %q, %v: every layout should give back what it took", exported.stdout, exported.err)
			}
			compacted := f.run(t, "relevance", "compact")
			if compacted.err != nil || !strings.Contains(compacted.stdout, "Compacted") {
				t.Fatalf("compact = %q, %v", compacted.stdout, compacted.err)
			}
		})
	}
}

// ledgerRecords names what the segments under the glob dir hold: nothing,
// reader verdicts, scores, or the whole ledger.
func ledgerRecords(t *testing.T, dir string) string {
	t.Helper()
	segments, err := filepath.Glob(filepath.Join(dir, "*.seg"))
	if err != nil {
		t.Fatal(err)
	}
	var batch model.RelevanceBatch
	for _, path := range segments {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		read, err := relevance.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		batch.Readers = append(batch.Readers, read.Readers...)
		batch.Scores = append(batch.Scores, read.Scores...)
	}
	switch {
	case len(batch.Readers) > 0 && len(batch.Scores) > 0:
		return "ledger"
	case len(batch.Readers) > 0:
		return "reader"
	case len(batch.Scores) > 0:
		return "scores"
	}
	return ""
}
