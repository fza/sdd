package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	llmrelevance "github.com/networkteam/sdd/pkg/llm/relevance"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const (
	rescoreFirst  = "20260201-000000-d-tac-one"
	rescoreSecond = "20260202-000000-d-tac-two"
	rescoreHost   = "chat:test-host"
)

// rescoreFixture is a graph of two referencing entries, two references each,
// seeded as labels for gather's judge prefilter and the Claude host's judge
// host to score; claudeCLI reports the host judge's claude CLI installed.
func rescoreFixture(t *testing.T, prefilter, host llmrelevance.Scorer, claudeCLI bool) (*sdd.Application, *localadapter.MemoryRelevanceStore) {
	t.Helper()
	f := newGatherFixture(t)
	for s, subject := range []string{rescoreFirst, rescoreSecond} {
		var refs strings.Builder
		refs.WriteString("intent: pending\nrefs:\n")
		for i := range 2 {
			id := rescoreRef(s, i)
			f.write(t, id, "gap", "Referenced gap "+id+".", "")
			fmt.Fprintf(&refs, "    - id: %s\n      kind: grounded-in\n      desc: a referenced gap\n", id)
		}
		f.write(t, subject, "directive", "The decision "+subject+".", refs.String())
	}
	store := localadapter.NewMemoryRelevanceStore()
	f.relevance = store
	f.prefilter = sdd.Prefilter{Scorer: prefilter, Judge: gatherJudge}
	f.ranking = sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: rescoreHost}, ClaudeCLI: claudeCLI}
	return f.app(t, prefilterPolicy(1), nil), store
}

// rescoreRef is the i-th entry the s-th subject of rescoreFixture refs.
func rescoreRef(s, i int) string {
	return fmt.Sprintf("20260101-0000%d%d-s-tac-ref", s, i)
}

// A subject whose requests fail is recorded and the pass goes on: the other
// subject is scored, and the failed pairs stay unscored for the next
// rescore.
func TestRescoreRelevance_GoesOnPastAFailingSubject(t *testing.T) {
	prefilter := &scriptedScorer{fail: map[string]bool{rescoreRef(0, 0): true}}
	application, _ := rescoreFixture(t, prefilter, &scriptedScorer{}, false)
	identity := sdd.RequestIdentity{Subject: "tester"}
	if _, err := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{}); err != nil {
		t.Fatal(err)
	}

	result, err := application.RescoreRelevance(t.Context(), identity, gatherProject, sdd.RescoreRequest{})
	if err != nil {
		t.Fatalf("rescore = %v: a failing subject should not end the pass", err)
	}
	if result.Judge != gatherJudge || result.Scored != 2 || result.Failed != 2 || len(result.Failures) != 1 || result.Failures[0].Subject != rescoreFirst || result.Failures[0].Err == nil {
		t.Fatalf("result %+v: the failing subject should be recorded with its cause and the other scored", result)
	}

	prefilter.fail = nil
	again, err := application.RescoreRelevance(t.Context(), identity, gatherProject, sdd.RescoreRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Scored != 2 || again.Failed != 0 {
		t.Fatalf("result %+v: the next rescore should score the pairs the failure left", again)
	}
}

// A cancelled pass stops at once instead of recording every remaining
// subject as failed.
func TestRescoreRelevance_StopsWhenCancelled(t *testing.T) {
	prefilter := &scriptedScorer{slow: map[string]bool{rescoreRef(0, 0): true, rescoreRef(1, 0): true}}
	application, _ := rescoreFixture(t, prefilter, &scriptedScorer{}, false)
	identity := sdd.RequestIdentity{Subject: "tester"}
	if _, err := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := application.RescoreRelevance(ctx, identity, gatherProject, sdd.RescoreRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rescore = %v: a cancelled pass should stop with its cause", err)
	}
}

// Without --judge the ranking judge of a reading without a client scores:
// the Claude host's while its claude CLI is installed, else the prefilter's.
// --judge names another, a legacy name included; the judge not selected
// sends no request.
func TestRescoreRelevance_ScoresWithOneJudge(t *testing.T) {
	for _, tc := range []struct {
		name      string
		judge     string
		claudeCLI bool
		wantHost  bool
	}{
		{name: "default with the claude CLI", claudeCLI: true, wantHost: true},
		{name: "default without the claude CLI"},
		{name: "host by name", judge: rescoreHost, wantHost: true},
		{name: "host by legacy name", judge: "listwise:test-host", wantHost: true},
		{name: "prefilter by name", judge: gatherJudge, claudeCLI: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefilter, host := &scriptedScorer{}, &scriptedScorer{}
			application, _ := rescoreFixture(t, prefilter, host, tc.claudeCLI)
			identity := sdd.RequestIdentity{Subject: "tester"}
			if _, err := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{Judge: tc.judge}); err != nil {
				t.Fatal(err)
			}
			result, err := application.RescoreRelevance(t.Context(), identity, gatherProject, sdd.RescoreRequest{Judge: tc.judge})
			if err != nil {
				t.Fatal(err)
			}
			want, idle, wantJudge := host, prefilter, rescoreHost
			if !tc.wantHost {
				want, idle, wantJudge = prefilter, host, gatherJudge
			}
			if result.Judge != wantJudge || result.Scored != 4 || want.calls() == 0 || idle.calls() != 0 {
				t.Fatalf("result %+v, %d idle calls: only %s should score", result, idle.calls(), wantJudge)
			}
		})
	}
}

// An unknown judge is refused before anything is seeded or scored, naming
// the judges there are; the former aliases name no judge.
func TestRescoreRelevance_RefusesAnUnknownJudge(t *testing.T) {
	for _, name := range []string{"chat:nope", "ranking", "prefilter"} {
		t.Run(name, func(t *testing.T) {
			prefilter, host := &scriptedScorer{}, &scriptedScorer{}
			application, store := rescoreFixture(t, prefilter, host, true)
			identity := sdd.RequestIdentity{Subject: "tester"}
			_, seedErr := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{Judge: name})
			_, rescoreErr := application.RescoreRelevance(t.Context(), identity, gatherProject, sdd.RescoreRequest{Judge: name})
			for call, err := range map[string]error{"seed": seedErr, "rescore": rescoreErr} {
				if err == nil {
					t.Fatalf("%s with an unknown judge should be refused", call)
				}
				if want := fmt.Sprintf("%q: pass one of %s, %s", name, rescoreHost, gatherJudge); !strings.Contains(err.Error(), want) {
					t.Errorf("%s refusal %q lacks %q", call, err, want)
				}
			}
			if segments, _ := store.Segments(t.Context()); len(segments) != 0 || prefilter.calls()+host.calls() != 0 {
				t.Fatalf("%d segments, %d calls: a refused judge should seed and score nothing", len(segments), prefilter.calls()+host.calls())
			}
		})
	}
}

// halvedScorer answers a request of more than one candidate one score
// short, so a Halving judge splits it down to single candidates.
type halvedScorer struct{ *scriptedScorer }

func (s halvedScorer) Score(ctx context.Context, request llmrelevance.Request) (llmrelevance.Result, error) {
	result, err := s.scriptedScorer.Score(ctx, request)
	if err == nil && len(request.Candidates) > 1 {
		result.Scores = result.Scores[1:]
	}
	return result, err
}

// tooLongScorer takes no pair: every one is too long to score.
type tooLongScorer struct{ *scriptedScorer }

func (tooLongScorer) MaxPairTokens() int { return 1 }

// The pass reports its progress in pairs: the total before the first
// request, then each finished request, its failed pairs and those too long
// to score included, so the count ends at the total however the judge
// splits its requests.
func TestRescoreRelevance_ReportsProgressInPairs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scorer llmrelevance.Scorer
		want   sdd.RescoreProgress
	}{
		{name: "all scored", scorer: &scriptedScorer{}, want: sdd.RescoreProgress{Judge: gatherJudge, Done: 4, Total: 4}},
		{name: "a subject failing", scorer: &scriptedScorer{fail: map[string]bool{rescoreRef(0, 0): true}}, want: sdd.RescoreProgress{Judge: gatherJudge, Done: 4, Total: 4, Failed: 2}},
		{name: "requests halved", scorer: llmrelevance.Halving(halvedScorer{&scriptedScorer{maxBatch: 10}},&llmrelevance.MemoryBatchSizes{}, gatherJudge), want: sdd.RescoreProgress{Judge: gatherJudge, Done: 4, Total: 4}},
		{name: "pairs too long", scorer: tooLongScorer{&scriptedScorer{}}, want: sdd.RescoreProgress{Judge: gatherJudge, Done: 4, Total: 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			application, _ := rescoreFixture(t, tc.scorer, &scriptedScorer{}, false)
			identity := sdd.RequestIdentity{Subject: "tester"}
			if _, err := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{}); err != nil {
				t.Fatal(err)
			}
			var snapshots []sdd.RescoreProgress
			request := sdd.RescoreRequest{OnProgress: func(progress sdd.RescoreProgress) { snapshots = append(snapshots, progress) }}
			if _, err := application.RescoreRelevance(t.Context(), identity, gatherProject, request); err != nil {
				t.Fatal(err)
			}
			if len(snapshots) < 2 || snapshots[0] != (sdd.RescoreProgress{Judge: gatherJudge, Total: 4}) {
				t.Fatalf("snapshots %+v: the total should come before any pair is done", snapshots)
			}
			for i := 1; i < len(snapshots); i++ {
				if snapshots[i].Done < snapshots[i-1].Done || snapshots[i].Done > snapshots[i].Total {
					t.Fatalf("snapshots %+v: the count should only rise, up to the total", snapshots)
				}
			}
			if last := snapshots[len(snapshots)-1]; last != tc.want {
				t.Fatalf("last snapshot %+v, want %+v: every pair should count, the failed ones counted apart", last, tc.want)
			}
		})
	}
}
