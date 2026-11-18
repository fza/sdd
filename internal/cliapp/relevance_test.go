package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	"github.com/networkteam/sdd/pkg/local"
)

const (
	relevanceCandidate = "20261101-120000-s-tac-aaa"
	relevanceSubject   = "20261109-004506-d-cpt-udz"
)

// liveRelevance is a live reader verdict and a score of one pair.
func liveRelevance() model.RelevanceBatch {
	pair := model.NewRelevancePair(relevanceCandidate, relevanceSubject)
	return model.RelevanceBatch{
		Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1}}},
		Scores:  []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: "systemone:jev-1.14.0", Score: 17}}},
	}
}

// appendLedger appends batches to the machine-state ledger of the fixture's
// project, one segment each.
func appendLedger(t *testing.T, f *appFixture, batches ...model.RelevanceBatch) string {
	t.Helper()
	dir := filepath.Join(f.root, "state", "sdd", "relevance", "example.test", "cli")
	store, err := local.NewFilesystemRelevanceStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range batches {
		if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRelevance_LivesOnlyInMachineState(t *testing.T) {
	cases := []struct {
		name   string
		config string
	}{
		{"without a relevance block", ""},
		{"with the retired relevance block", "relevance:\n  commit: true\n  content: reader\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n"+tc.config)
			live := liveRelevance()
			state := appendLedger(t, f, model.RelevanceBatch{Readers: live.Readers}, model.RelevanceBatch{Scores: live.Scores})
			compacted := f.run(t, "relevance", "compact")
			if compacted.err != nil || !strings.Contains(compacted.stdout, "Compacted") {
				t.Fatalf("compact = %q, %v", compacted.stdout, compacted.err)
			}
			committed := ledgerRecords(t, filepath.Join(f.root, ".sdd", "relevance"))
			if committed != "" || ledgerRecords(t, state) != "ledger" || segmentCount(t, state) != 1 {
				t.Fatalf("committed holds %q, machine state %q in %d segments: compact should merge the machine-state ledger alone", committed, ledgerRecords(t, state), segmentCount(t, state))
			}
		})
	}
}

func segmentCount(t *testing.T, dir string) int {
	t.Helper()
	segments, err := filepath.Glob(filepath.Join(dir, "*.seg"))
	if err != nil {
		t.Fatal(err)
	}
	return len(segments)
}

// ledgerRecords names what the segments under the glob dir hold: nothing,
// reader verdicts, scores, or the whole ledger.
func ledgerRecords(t *testing.T, dir string) string {
	t.Helper()
	batch := ledgerBatch(t, dir)
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

// ledgerBatch is every record the segments under dir hold.
func ledgerBatch(t *testing.T, dir string) model.RelevanceBatch {
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
	return batch
}

func TestRelevanceCompact_DropsAReplaySeed(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n")
	seed := model.RelevanceBatch{Readers: []model.ReaderRecord{{Pair: model.NewRelevancePair("20261102-120000-s-tac-bbb", relevanceSubject), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced, Source: model.SourceReplay}}}}
	state := appendLedger(t, f, liveRelevance(), seed)
	dropped := f.run(t, "relevance", "compact", "--drop-replay")
	if dropped.err != nil || !strings.Contains(dropped.stdout, "without its replayed labels") {
		t.Fatalf("compact = %q, %v", dropped.stdout, dropped.err)
	}
	if got, want := ledgerBatch(t, state), liveRelevance(); len(got.Readers) != 1 || got.Readers[0] != want.Readers[0] || len(got.Scores) != 1 || got.Scores[0] != want.Scores[0] {
		t.Fatalf("ledger %+v: the seed should be gone and the live records kept", got)
	}
}

// The CLI neither imports nor exports labels: that stays a development
// script's job.
func TestRelevance_OffersNoLabelImportOrExport(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n")
	for _, args := range [][]string{{"relevance", "export"}, {"relevance", "import", "-"}, {"grounding", "seed", "--from", "-"}, {"grounding", "rescore"}} {
		if result := f.run(t, args...); result.err == nil {
			t.Errorf("sdd %v should be unknown, got:\n%s", args, result.stdout)
		}
	}
}

func TestRelevance_ReportsLeftoverSegmentsInTheRepository(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n")
	f.write(t, ".sdd/relevance/20261101T120000-abc.seg", "leftover")
	ledgerDir := filepath.Join(f.root, "state", "sdd", "relevance", "example.test", "cli")

	for _, args := range [][]string{{"info"}, {"config"}} {
		result := f.run(t, args...)
		if result.err != nil {
			t.Fatalf("%v: %v\n%s", args, result.err, result.stderr)
		}
		if !strings.Contains(result.stderr, "ignoring relevance segments in the repository") || !strings.Contains(result.stderr, "move them into "+ledgerDir) || !strings.Contains(result.stderr, "commit their removal") {
			t.Errorf("%v should report the leftover segments with the machine-state folder to move them into:\n%s", args, result.stderr)
		}
	}
	_, err := os.Stat(filepath.Join(f.root, ".sdd", "relevance", "20261101T120000-abc.seg"))
	if err != nil {
		t.Fatalf("a leftover segment should stay where it is: %v", err)
	}
}
