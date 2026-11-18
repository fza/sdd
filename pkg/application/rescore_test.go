package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const (
	rescoreFirst  = "20260201-000000-d-tac-one"
	rescoreSecond = "20260202-000000-d-tac-two"
	rescoreHost   = "listwise:test-host"
)

// rescoreFixture is a graph of two referencing entries, two references each,
// seeded as labels for gather's judge prefilter and the Claude host's judge
// host to score.
func rescoreFixture(t *testing.T, prefilter, host *scriptedScorer) (*sdd.Application, *localadapter.MemoryRelevanceStore) {
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
	f.ranking = sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: rescoreHost}}
	return f.app(t, prefilterPolicy(1), nil), store
}

// rescoreRef is the i-th entry the s-th subject of rescoreFixture refs.
func rescoreRef(s, i int) string {
	return fmt.Sprintf("20260101-0000%d%d-s-tac-ref", s, i)
}

func rescoreResult(results []sdd.RescoreResult, judge string) sdd.RescoreResult {
	for _, result := range results {
		if result.Judge == judge {
			return result
		}
	}
	return sdd.RescoreResult{}
}

// A subject whose requests fail is recorded and the pass goes on: the other
// subjects and the other judge are scored, and the failed pairs stay
// unscored for the next rescore.
func TestRescoreRelevance_GoesOnPastAFailingSubject(t *testing.T) {
	prefilter := &scriptedScorer{fail: map[string]bool{rescoreRef(0, 0): true}}
	host := &scriptedScorer{}
	application, _ := rescoreFixture(t, prefilter, host)
	identity := sdd.RequestIdentity{Subject: "tester"}
	if _, err := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{}); err != nil {
		t.Fatal(err)
	}

	results, err := application.RescoreRelevance(t.Context(), identity, gatherProject, sdd.RescoreRequest{})
	if err != nil {
		t.Fatalf("rescore = %v: a failing subject should not end the pass", err)
	}
	gathers := rescoreResult(results, gatherJudge)
	if gathers.Scored != 2 || gathers.Failed != 2 || len(gathers.Failures) != 1 || gathers.Failures[0].Subject != rescoreFirst || gathers.Failures[0].Err == nil {
		t.Fatalf("gather's judge %+v: the failing subject should be recorded with its cause and the other scored", gathers)
	}
	if hosts := rescoreResult(results, rescoreHost); hosts.Scored != 4 || len(hosts.Failures) != 0 {
		t.Fatalf("host judge %+v: the other judge should score every pair", hosts)
	}

	prefilter.fail = nil
	again, err := application.RescoreRelevance(t.Context(), identity, gatherProject, sdd.RescoreRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if gathers := rescoreResult(again, gatherJudge); gathers.Scored != 2 || gathers.Failed != 0 {
		t.Fatalf("gather's judge %+v: the next rescore should score the pairs the failure left", gathers)
	}
}

// A cancelled pass stops at once instead of recording every remaining
// subject as failed.
func TestRescoreRelevance_StopsWhenCancelled(t *testing.T) {
	prefilter := &scriptedScorer{slow: map[string]bool{rescoreRef(0, 0): true, rescoreRef(1, 0): true}}
	application, _ := rescoreFixture(t, prefilter, &scriptedScorer{})
	identity := sdd.RequestIdentity{Subject: "tester"}
	if _, err := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := application.RescoreRelevance(ctx, identity, gatherProject, sdd.RescoreRequest{Judge: sdd.JudgeAliasPrefilter})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rescore = %v: a cancelled pass should stop with its cause", err)
	}
}

// --judge scores with the one judge it names, by name or alias; the other
// judge sends no request.
func TestRescoreRelevance_ScoresWithTheNamedJudgeAlone(t *testing.T) {
	for _, tc := range []struct {
		judge    string
		wantHost bool
	}{
		{judge: sdd.JudgeAliasRanking, wantHost: true},
		{judge: rescoreHost, wantHost: true},
		{judge: sdd.JudgeAliasPrefilter},
		{judge: gatherJudge},
	} {
		t.Run(tc.judge, func(t *testing.T) {
			prefilter, host := &scriptedScorer{}, &scriptedScorer{}
			application, _ := rescoreFixture(t, prefilter, host)
			identity := sdd.RequestIdentity{Subject: "tester"}
			if _, err := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{Judge: tc.judge}); err != nil {
				t.Fatal(err)
			}
			results, err := application.RescoreRelevance(t.Context(), identity, gatherProject, sdd.RescoreRequest{Judge: tc.judge})
			if err != nil {
				t.Fatal(err)
			}
			want, idle, wantJudge := host, prefilter, rescoreHost
			if !tc.wantHost {
				want, idle, wantJudge = prefilter, host, gatherJudge
			}
			if len(results) != 1 || results[0].Judge != wantJudge || results[0].Scored != 4 || want.calls() == 0 || idle.calls() != 0 {
				t.Fatalf("results %+v, %d idle calls: only %s should score", results, idle.calls(), wantJudge)
			}
		})
	}
}

// An unknown judge is refused before anything is seeded or scored, naming
// the judges and aliases there are.
func TestRescoreRelevance_RefusesAnUnknownJudge(t *testing.T) {
	prefilter, host := &scriptedScorer{}, &scriptedScorer{}
	application, store := rescoreFixture(t, prefilter, host)
	identity := sdd.RequestIdentity{Subject: "tester"}
	_, seedErr := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{Judge: "listwise:nope"})
	_, rescoreErr := application.RescoreRelevance(t.Context(), identity, gatherProject, sdd.RescoreRequest{Judge: "listwise:nope"})
	for name, err := range map[string]error{"seed": seedErr, "rescore": rescoreErr} {
		if err == nil {
			t.Fatalf("%s with an unknown judge should be refused", name)
		}
		for _, want := range []string{`"listwise:nope"`, gatherJudge + " (prefilter)", rescoreHost + " (ranking)"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s refusal %q lacks %q", name, err, want)
			}
		}
	}
	if segments, _ := store.Segments(t.Context()); len(segments) != 0 || prefilter.calls()+host.calls() != 0 {
		t.Fatalf("%d segments, %d calls: a refused judge should seed and score nothing", len(segments), prefilter.calls()+host.calls())
	}
}

// With every judge, the listwise judge scores first, so a slow per-pair
// judge never holds it back.
func TestRescoreRelevance_ScoresWithTheListwiseJudgeFirst(t *testing.T) {
	application, _ := rescoreFixture(t, &scriptedScorer{}, &scriptedScorer{})
	identity := sdd.RequestIdentity{Subject: "tester"}
	if _, err := application.SeedRefLabels(t.Context(), identity, gatherProject, sdd.RescoreRequest{}); err != nil {
		t.Fatal(err)
	}
	results, err := application.RescoreRelevance(t.Context(), identity, gatherProject, sdd.RescoreRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Judge != rescoreHost || results[1].Judge != gatherJudge {
		t.Fatalf("results %+v: the listwise judge should score first", results)
	}
}
