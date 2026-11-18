package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	llmrelevance "github.com/networkteam/sdd/pkg/llm/relevance"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const (
	updateFirst  = "20260201-000000-d-tac-one"
	updateSecond = "20260202-000000-d-tac-two"
	updateHost   = "chat:test-host"
)

// updateFixture is a graph of two referencing entries, two references each,
// for gather's judge prefilter or the Claude host's judge host to score;
// claudeCLI reports the host judge's claude CLI installed, which makes it
// the ranking judge.
func updateFixture(t *testing.T, prefilter, host llmrelevance.Scorer, claudeCLI bool) (*sdd.Application, *localadapter.MemoryRelevanceStore) {
	t.Helper()
	f := newGatherFixture(t)
	for s, subject := range []string{updateFirst, updateSecond} {
		var refs strings.Builder
		refs.WriteString("intent: pending\nrefs:\n")
		for i := range 2 {
			id := updateRef(s, i)
			f.write(t, id, "gap", "Referenced gap "+id+".", "")
			fmt.Fprintf(&refs, "    - id: %s\n      kind: grounded-in\n      desc: a referenced gap\n", id)
		}
		f.write(t, subject, "directive", "The decision "+subject+".", refs.String())
	}
	store := localadapter.NewMemoryRelevanceStore()
	f.relevance = store
	f.prefilter = sdd.Prefilter{Scorer: prefilter, Judge: gatherJudge}
	f.ranking = sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: updateHost}, ClaudeCLI: claudeCLI}
	return f.app(t, prefilterPolicy(1), nil), store
}

// updateRef is the i-th entry the s-th subject of updateFixture refs.
func updateRef(s, i int) string {
	return fmt.Sprintf("20260101-0000%d%d-s-tac-ref", s, i)
}

var updateIdentity = sdd.RequestIdentity{Subject: "tester"}

// A subject whose requests fail is recorded and the pass goes on: the other
// subject is scored, and the failed pairs stay unscored for the next update.
func TestUpdateGrounding_GoesOnPastAFailingSubject(t *testing.T) {
	prefilter := &scriptedScorer{fail: map[string]bool{updateRef(0, 0): true}}
	application, _ := updateFixture(t, prefilter, &scriptedScorer{}, false)

	result, err := application.UpdateGrounding(t.Context(), updateIdentity, gatherProject, sdd.GroundingUpdateRequest{})
	if err != nil {
		t.Fatalf("update = %v: a failing subject should not end the pass", err)
	}
	scored := result.Scored
	if result.Seeded.Added != 4 || scored.Judge != gatherJudge || scored.Scored != 2 || scored.Failed != 2 || len(scored.Failures) != 1 || scored.Failures[0].Subject != updateFirst || scored.Failures[0].Err == nil {
		t.Fatalf("result %+v: the failing subject should be recorded with its cause and the other scored", result)
	}

	prefilter.fail = nil
	again, err := application.UpdateGrounding(t.Context(), updateIdentity, gatherProject, sdd.GroundingUpdateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Seeded.Added != 0 || again.Seeded.Skipped != 4 || again.Scored.Scored != 2 || again.Scored.Failed != 0 {
		t.Fatalf("result %+v: the next update should label nothing and score the pairs the failure left", again)
	}
}

// A cancelled pass stops at once instead of recording every remaining
// subject as failed.
func TestUpdateGrounding_StopsWhenCancelled(t *testing.T) {
	prefilter := &scriptedScorer{slow: map[string]bool{updateRef(0, 0): true, updateRef(1, 0): true}}
	application, _ := updateFixture(t, prefilter, &scriptedScorer{}, false)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := application.UpdateGrounding(ctx, updateIdentity, gatherProject, sdd.GroundingUpdateRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("update = %v: a cancelled pass should stop with its cause", err)
	}
}

// The ranking judge of a reading without a client scores alone: the Claude
// host's while its claude CLI is installed, else the prefilter's.
func TestUpdateGrounding_ScoresWithTheRankingJudge(t *testing.T) {
	for _, tc := range []struct {
		name      string
		claudeCLI bool
	}{
		{name: "with the claude CLI", claudeCLI: true},
		{name: "without the claude CLI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefilter, host := &scriptedScorer{}, &scriptedScorer{}
			application, _ := updateFixture(t, prefilter, host, tc.claudeCLI)
			result, err := application.UpdateGrounding(t.Context(), updateIdentity, gatherProject, sdd.GroundingUpdateRequest{})
			if err != nil {
				t.Fatal(err)
			}
			want, idle, wantJudge := host, prefilter, updateHost
			if !tc.claudeCLI {
				want, idle, wantJudge = prefilter, host, gatherJudge
			}
			if result.Scored.Judge != wantJudge || result.Scored.Scored != 4 || want.calls() == 0 || idle.calls() != 0 {
				t.Fatalf("result %+v, %d idle calls: only %s should score", result, idle.calls(), wantJudge)
			}
		})
	}
}

// Force scores every labelled pair again, its new score standing even below
// the old one; without it a scored pair is not sent again.
func TestUpdateGrounding_ForceRescoresEveryPair(t *testing.T) {
	prefilter := &scriptedScorer{}
	application, store := updateFixture(t, prefilter, &scriptedScorer{}, false)
	if _, err := application.UpdateGrounding(t.Context(), updateIdentity, gatherProject, sdd.GroundingUpdateRequest{}); err != nil {
		t.Fatal(err)
	}
	prefilter.scores = map[string]float64{}
	for s := range 2 {
		for i := range 2 {
			prefilter.scores[updateRef(s, i)] = 0.1
		}
	}
	calls := prefilter.calls()
	plain, err := application.UpdateGrounding(t.Context(), updateIdentity, gatherProject, sdd.GroundingUpdateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Scored.Scored != 0 || prefilter.calls() != calls {
		t.Fatalf("result %+v: an update without force should send no scored pair", plain)
	}
	forced, err := application.UpdateGrounding(t.Context(), updateIdentity, gatherProject, sdd.GroundingUpdateRequest{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if forced.Scored.Scored != 4 {
		t.Fatalf("result %+v: force should score every labelled pair", forced)
	}
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	pair := model.NewRelevancePair(updateFirst, updateRef(0, 0))
	if score, ok := ledger.Score(pair, updateFirst, gatherJudge); !ok || score != model.ScoreByte(0.1) {
		t.Fatalf("score = %d, %v: the forced score should replace the higher old one", score, ok)
	}
}

// appendOnlyStore is a relevance store that cannot replace scores.
type appendOnlyStore struct{ sdd.RelevanceStore }

// A store that cannot replace scores refuses a forced update before any
// request: its old scores would outvote the new ones.
func TestUpdateGrounding_ForceNeedsAStoreThatReplacesScores(t *testing.T) {
	f := newGatherFixture(t)
	f.relevance = appendOnlyStore{localadapter.NewMemoryRelevanceStore()}
	prefilter := &scriptedScorer{}
	f.prefilter = sdd.Prefilter{Scorer: prefilter, Judge: gatherJudge}
	application := f.app(t, prefilterPolicy(1), nil)
	_, err := application.UpdateGrounding(t.Context(), updateIdentity, gatherProject, sdd.GroundingUpdateRequest{Force: true})
	if err == nil || !strings.Contains(err.Error(), "cannot replace scores") || prefilter.calls() != 0 {
		t.Fatalf("forced update = %v after %d calls: should be refused before scoring", err, prefilter.calls())
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
func TestUpdateGrounding_ReportsProgressInPairs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scorer llmrelevance.Scorer
		want   sdd.RescoreProgress
	}{
		{name: "all scored", scorer: &scriptedScorer{}, want: sdd.RescoreProgress{Judge: gatherJudge, Done: 4, Total: 4}},
		{name: "a subject failing", scorer: &scriptedScorer{fail: map[string]bool{updateRef(0, 0): true}}, want: sdd.RescoreProgress{Judge: gatherJudge, Done: 4, Total: 4, Failed: 2}},
		{name: "requests halved", scorer: llmrelevance.Halving(halvedScorer{&scriptedScorer{maxBatch: 10}}, &llmrelevance.MemoryBatchSizes{}, gatherJudge), want: sdd.RescoreProgress{Judge: gatherJudge, Done: 4, Total: 4}},
		{name: "pairs too long", scorer: tooLongScorer{&scriptedScorer{}}, want: sdd.RescoreProgress{Judge: gatherJudge, Done: 4, Total: 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			application, _ := updateFixture(t, tc.scorer, &scriptedScorer{}, false)
			var snapshots []sdd.RescoreProgress
			request := sdd.GroundingUpdateRequest{OnProgress: func(progress sdd.RescoreProgress) { snapshots = append(snapshots, progress) }}
			if _, err := application.UpdateGrounding(t.Context(), updateIdentity, gatherProject, request); err != nil {
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
