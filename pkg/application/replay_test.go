package application_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	llmrelevance "github.com/networkteam/sdd/pkg/llm/relevance"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// flaggingScan flags every hidden entry that is a referenced gap of the
// auto fixture, and records what each call read.
type flaggingScan struct {
	mu    sync.Mutex
	calls [][]string
}

func (s *flaggingScan) Scan(_ context.Context, _ string, hidden []llmrelevance.Candidate) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var read, flagged []string
	for _, candidate := range hidden {
		read = append(read, candidate.ID)
		if strings.HasSuffix(candidate.ID, "-s-tac-ref") {
			flagged = append(flagged, candidate.ID)
		}
	}
	s.calls = append(s.calls, read)
	return flagged, nil
}

func proofRecordsOf(t *testing.T, store *localadapter.MemoryProofStore) model.ProofRecords {
	t.Helper()
	segments, err := store.Segments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := relevance.FoldProofs(segments)
	if err != nil {
		t.Fatal(err)
	}
	return records
}

// replaySubjects decisions of replayFixture each reference two gaps of
// their own; every entry shares the topic cli/ux, so a replay's topic
// listing holds every earlier entry.
const replaySubjects = 8

func replayFixture(t *testing.T) *autoFixture {
	t.Helper()
	f := &autoFixture{gatherFixture: newGatherFixture(t), store: localadapter.NewMemoryRelevanceStore()}
	for s := range replaySubjects {
		refs := "intent: pending\ntopics:\n  - cli/ux\nrefs:\n"
		for i := range 2 {
			f.write(t, autoRef(s, i), "gap", "Referenced gap "+autoRef(s, i)+".", "topics:\n  - cli/ux\n")
			refs += "    - id: " + autoRef(s, i) + "\n      kind: grounded-in\n      desc: a referenced gap\n"
		}
		f.write(t, autoSubject(s), "directive", "The decision "+autoSubject(s)+".", refs)
	}
	runs, err := localadapter.NewFilesystemCalibrationRuns(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.runs, f.gatherFixture.runs, f.relevance = runs, runs, f.store
	return f
}

// With nothing left to score, the server scans the captures whose check hid
// entries, then replays past entries as whole checks against the entries
// with earlier IDs, each once, with a planted miss the scan reads, until
// none is left to replay.
func TestAutoCalibrate_ScansWrittenEntriesThenReplaysPastOnes(t *testing.T) {
	f := replayFixture(t)
	proofs := localadapter.NewMemoryProofStore()
	f.proofs = proofs
	written := autoSubject(0)
	seed, err := relevance.EncodeProofs(model.ProofRecords{Tallies: []model.CheckTally{{
		Capture: written, Judge: gatherJudge, At: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		Rules: []model.RuleTally{{Rule: model.RuleFarFloor, Step: model.StepNarrowed, Hidden: []string{autoRef(0, 1), autoSubject(1)}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := proofs.Append(t.Context(), seed); err != nil {
		t.Fatal(err)
	}
	scorer := &scriptedScorer{}
	scan := &flaggingScan{}
	f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: gatherJudge, Revision: "r1", Scan: scan}
	policy := prefilterPolicy(1)
	policy.AutoCalibrate = true
	application := f.app(t, policy, &countingEmbeddings{})
	startAutoCalibrate(t, application, sdd.AutoCalibrationRequest{})

	// The first replay's search indexes the graph, which takes a while.
	deadline := time.Now().Add(90 * time.Second)
	for len(proofRecordsOf(t, proofs).Replays) < replaySubjects {
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d subjects replayed in time", len(proofRecordsOf(t, proofs).Replays), replaySubjects)
		}
		time.Sleep(20 * time.Millisecond)
	}
	records := proofRecordsOf(t, proofs)
	if len(records.Scans) != 1 || records.Scans[0].Capture != written {
		t.Fatalf("scans %+v: the written capture should be scanned once", records.Scans)
	}
	if len(records.Misses) != 1 || records.Misses[0] != (model.Miss{Capture: written, Hidden: autoRef(0, 1), Rule: model.RuleFarFloor, Detector: model.DetectScan, Judge: gatherJudge, At: records.Misses[0].At}) {
		t.Fatalf("misses %+v: the flagged hidden entry should be a scan miss of the rule that hid it", records.Misses)
	}
	seen := map[string]bool{}
	for _, check := range records.Replays {
		if seen[check.Subject] {
			t.Fatalf("%s was replayed twice", check.Subject)
		}
		seen[check.Subject] = true
		if check.Judge != gatherJudge || check.Revision != "r1" || check.GraphSize == 0 {
			t.Fatalf("check %+v should name judge, revision and graph size", check)
		}
		var needed []string
		for _, candidate := range check.Candidates {
			if candidate.ID >= check.Subject {
				t.Fatalf("candidate %s of %s: a replay reads only entries with earlier IDs", candidate.ID, check.Subject)
			}
			if candidate.Needed {
				needed = append(needed, candidate.ID)
			}
		}
		if len(needed) == 0 || !slices.Contains(needed, check.Planted) || !check.Caught {
			t.Fatalf("check of %s: its references should be needed candidates, one planted and caught by the scan", check.Subject)
		}
	}
}

// Without a store of proof records or without the semantic search a server
// replays nothing.
func TestAutoCalibrate_ReplaysNothingWithoutAProofStoreOrAnEmbedder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		proofs bool
		embed  bool
	}{{name: "no proof store", embed: true}, {name: "no embedder", proofs: true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAutoFixture(t)
			if tc.proofs {
				f.proofs = localadapter.NewMemoryProofStore()
			}
			scorer := &scriptedScorer{}
			f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: gatherJudge}
			policy := prefilterPolicy(1)
			policy.AutoCalibrate = true
			var application *sdd.Application
			if tc.embed {
				application = f.app(t, policy, &countingEmbeddings{})
			} else {
				application = f.app(t, policy, nil)
			}
			stop := startAutoCalibrate(t, application, sdd.AutoCalibrationRequest{})
			eventually(t, "the top stage holds", func() bool { return f.stage(t) == model.RecallFloor })
			// Requests in flight when the stage was reached still return.
			time.Sleep(200 * time.Millisecond)
			calls := scorer.calls()
			time.Sleep(200 * time.Millisecond)
			stop()
			if scorer.calls() != calls {
				t.Fatalf("%d calls after the top stage: nothing should be replayed", scorer.calls()-calls)
			}
		})
	}
}

// A replay sends no request while a graph check's judge scores: the server
// hands AutoCalibrate the application's WaitNoLiveCheck, and the replay
// resumes once the check is done.
func TestAutoCalibrate_AReplayWaitsWhileACheckJudges(t *testing.T) {
	f := replayFixture(t)
	f.proofs = localadapter.NewMemoryProofStore()
	scorer := &heldCheckScorer{scriptedScorer: &scriptedScorer{}, checking: make(chan struct{}, 1), release: make(chan struct{})}
	f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: gatherJudge, Revision: "r1", Scan: &flaggingScan{}}
	policy := prefilterPolicy(1)
	policy.AutoCalibrate = true
	application := f.app(t, policy, &countingEmbeddings{})
	// The gate holds the first replay until the check judges, so a replay is
	// due while it does.
	reached, gate := make(chan struct{}, 1), make(chan struct{})
	startAutoCalibrate(t, application, sdd.AutoCalibrationRequest{WaitNoLiveCheck: func(ctx context.Context) error {
		select {
		case reached <- struct{}{}:
		default:
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		return application.WaitNoLiveCheck(ctx)
	}})
	select {
	case <-reached:
	case <-time.After(90 * time.Second):
		t.Fatal("no replay came due in time")
	}

	checked := make(chan error, 1)
	go func() {
		_, err := gather(t, application, sdd.GatherRequest{Draft: "A check draft about referenced gaps.", Terms: []string{"Referenced"}})
		checked <- err
	}()
	select {
	case <-scorer.checking:
	case <-time.After(10 * time.Second):
		t.Fatal("the check's judge should be scoring")
	}
	calls := scorer.calibrationCalls()
	close(gate)
	time.Sleep(300 * time.Millisecond)
	if held := scorer.calibrationCalls() - calls; held != 0 {
		t.Fatalf("%d replay calls while the check's judge scored: a replay should wait", held)
	}
	close(scorer.release)
	if err := <-checked; err != nil {
		t.Fatal(err)
	}
	eventually(t, "the replay resumes once the check is done", func() bool { return scorer.calibrationCalls() > calls })
}
