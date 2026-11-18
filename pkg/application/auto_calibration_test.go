package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const (
	autoSubjects = 40
	autoRefs     = 5
)

func autoSubject(s int) string { return fmt.Sprintf("20260301-%06d-d-tac-sub", s) }
func autoRef(s, i int) string  { return fmt.Sprintf("20260201-%04d%02d-s-tac-ref", s, i) }

// autoFixture is a graph of autoSubjects decisions, each referencing
// autoRefs gaps of its own: 200 references, enough for the top recall stage
// once labelled and scored.
type autoFixture struct {
	*gatherFixture
	store *localadapter.MemoryRelevanceStore
	runs  *localadapter.FilesystemCalibrationRuns
}

func newAutoFixture(t *testing.T) *autoFixture {
	t.Helper()
	f := &autoFixture{gatherFixture: newGatherFixture(t), store: localadapter.NewMemoryRelevanceStore()}
	for s := range autoSubjects {
		var refs strings.Builder
		refs.WriteString("intent: pending\nrefs:\n")
		for i := range autoRefs {
			f.write(t, autoRef(s, i), "gap", "Referenced gap "+autoRef(s, i)+".", "")
			fmt.Fprintf(&refs, "    - id: %s\n      kind: grounded-in\n      desc: a referenced gap\n", autoRef(s, i))
		}
		f.write(t, autoSubject(s), "directive", "The decision "+autoSubject(s)+".", refs.String())
	}
	runs, err := localadapter.NewFilesystemCalibrationRuns(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.runs, f.gatherFixture.runs, f.relevance = runs, runs, f.store
	return f
}

func (f *autoFixture) application(t *testing.T, scorer *scriptedScorer, on bool) *sdd.Application {
	f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: gatherJudge}
	policy := prefilterPolicy(1)
	policy.AutoCalibrate = on
	return f.app(t, policy, nil)
}

// start runs AutoCalibrate until the test ends or stop is called, which
// returns once AutoCalibrate did.
func startAutoCalibrate(t *testing.T, application *sdd.Application, request sdd.AutoCalibrationRequest) (stop func()) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		application.AutoCalibrate(ctx, updateIdentity, gatherProject, request)
	}()
	stop = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("AutoCalibrate did not return within 5s of its context ending")
		}
	}
	t.Cleanup(stop)
	return stop
}

func (f *autoFixture) ledger(t *testing.T) *model.RelevanceLedger {
	t.Helper()
	segments, err := f.store.Segments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func (f *autoFixture) stage(t *testing.T) float64 {
	ledger := f.ledger(t)
	return model.Calibrate(ledger.Labels(gatherJudge), ledger.Steps(), gatherJudge).Stage
}

func eventually(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// On a ledger without labels the server labels the graph's own references,
// scores until the first stage holds, then sends a further request only
// once the server is idle, up to the top stage.
func TestAutoCalibrate_ReachesTheFirstStageThenScoresOnlyWhileIdle(t *testing.T) {
	f := newAutoFixture(t)
	scorer := &scriptedScorer{}
	application := f.application(t, scorer, true)
	idle, waiting := make(chan struct{}), make(chan struct{}, 1)
	startAutoCalibrate(t, application, sdd.AutoCalibrationRequest{WaitIdle: func(ctx context.Context) error {
		select {
		case waiting <- struct{}{}:
		default:
		}
		select {
		case <-idle:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})

	select {
	case <-waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("the run should wait for an idle server once the first stage holds")
	}
	time.Sleep(50 * time.Millisecond)
	calls := scorer.calls()
	time.Sleep(50 * time.Millisecond)
	if got := f.stage(t); got != 0.90 || scorer.calls() != calls {
		t.Fatalf("stage %.2f after %d then %d calls: the run should hold at the first stage until the server is idle", got, calls, scorer.calls())
	}
	run, running, err := f.runs.Running(t.Context())
	if err != nil || !running || !run.Waiting || run.Stage != 0.90 || run.Judge != gatherJudge || run.Scored == 0 {
		t.Fatalf("run %+v, %v, %v: the run should record its stage and its wait", run, running, err)
	}

	close(idle)
	eventually(t, "the top stage holds", func() bool { return f.stage(t) == model.RecallFloor })
	eventually(t, "the run ends", func() bool {
		_, running, err := f.runs.Running(t.Context())
		return err == nil && !running
	})
	if tally := f.ledger(t).Tally(false); tally.Referenced != autoSubjects*autoRefs {
		t.Fatalf("tally %+v: the graph's own references should be labelled first", tally)
	}
	for _, request := range scorer.requests {
		if len(request.Candidates) != autoRefs || !strings.Contains(request.Subject, "The decision") {
			t.Fatalf("request of %d candidates: each subject's labelled candidates should go together", len(request.Candidates))
		}
	}
}

// A server that quits stops its requests in flight at once and keeps every
// score a finished request returned; the next server scores only the pairs
// still unscored.
func TestAutoCalibrate_StopsWithTheServerAndResumesWithoutPayingTwice(t *testing.T) {
	f := newAutoFixture(t)
	first := &scriptedScorer{slow: map[string]bool{}}
	for s := range autoSubjects - 4 {
		first.slow[autoRef(s, 0)] = true
	}
	stop := startAutoCalibrate(t, f.application(t, first, true), sdd.AutoCalibrationRequest{})
	eventually(t, "the four latest subjects are scored", func() bool { return len(f.ledger(t).Labels(gatherJudge)) == 4*autoRefs })
	started := time.Now()
	stop()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stopping took %v: requests in flight should end with the server", elapsed)
	}
	scored := map[string]bool{}
	for _, label := range f.ledger(t).Labels(gatherJudge) {
		scored[label.Pair.A] = true
	}
	if len(scored) != 4*autoRefs {
		t.Fatalf("%d pairs scored: exactly the finished requests' scores should be kept", len(scored))
	}

	second := &scriptedScorer{}
	startAutoCalibrate(t, f.application(t, second, true), sdd.AutoCalibrationRequest{})
	eventually(t, "the top stage holds", func() bool { return f.stage(t) == model.RecallFloor })
	for _, request := range second.requests {
		for _, candidate := range request.Candidates {
			if scored[candidate.ID] {
				t.Fatalf("%s was scored again: a restart should pay for nothing twice", candidate.ID)
			}
		}
	}
}

// Nothing is sent while the switch is off, while another server holds the
// run, or once the judge holds the top stage.
func TestAutoCalibrate_SendsNothingWhenItHasNothingToDo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setUp func(t *testing.T, f *autoFixture) bool
	}{
		{name: "switched off", setUp: func(*testing.T, *autoFixture) bool { return false }},
		{name: "another server holds the run", setUp: func(t *testing.T, f *autoFixture) bool {
			lease, ok, err := f.runs.Acquire(t.Context())
			if err != nil || !ok {
				t.Fatalf("acquire = %v, %v", ok, err)
			}
			t.Cleanup(func() { _ = lease.Release() })
			return true
		}},
		{name: "the top stage holds", setUp: func(t *testing.T, f *autoFixture) bool {
			if _, err := f.application(t, &scriptedScorer{}, false).UpdateGrounding(t.Context(), updateIdentity, gatherProject, sdd.GroundingUpdateRequest{}); err != nil {
				t.Fatal(err)
			}
			if f.stage(t) != model.RecallFloor {
				t.Fatal("the update should reach the top stage")
			}
			return true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAutoFixture(t)
			on := tc.setUp(t, f)
			scorer := &scriptedScorer{}
			stop := startAutoCalibrate(t, f.application(t, scorer, on), sdd.AutoCalibrationRequest{})
			time.Sleep(200 * time.Millisecond)
			stop()
			if scorer.calls() != 0 {
				t.Fatalf("%d calls: nothing should be sent", scorer.calls())
			}
		})
	}
}

// `sdd grounding status` names the running background calibration.
func TestGroundingStatus_NamesTheBackgroundRun(t *testing.T) {
	f := newAutoFixture(t)
	application := f.application(t, &scriptedScorer{}, true)
	lease, ok, err := f.runs.Acquire(t.Context())
	if err != nil || !ok {
		t.Fatalf("acquire = %v, %v", ok, err)
	}
	t.Cleanup(func() { _ = lease.Release() })
	if err := lease.Record(t.Context(), sdd.CalibrationRun{Judge: gatherJudge, Started: time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC), Stage: 0.95, Scored: 80}); err != nil {
		t.Fatal(err)
	}
	status, err := application.GroundingStatus(t.Context(), updateIdentity, gatherProject, sdd.GroundingStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Text, "auto:    running in `sdd serve` since 2026-11-18 09:30 UTC: 80 pairs scored, stage 0.95 held") {
		t.Fatalf("status should name the running calibration:\n%s", status.Text)
	}
}
