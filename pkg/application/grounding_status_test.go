package application_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// Replayed labels count toward the calibration until the status excludes
// them, and the status names both sources either way.
func TestGroundingStatus_ExcludesReplayedLabelsOnRequest(t *testing.T) {
	var batch model.RelevanceBatch
	for i := range 189 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-pos", i), "20250601-000000-d-tac-sub")
		source := model.SourceLive
		if i%2 == 0 {
			source = model.SourceReplay
		}
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced, Source: source}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: gatherJudge, Score: 200}})
	}
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	f := prefilterFixture(t, &scriptedScorer{})
	f.relevance = store
	application := f.app(t, prefilterPolicy(1), nil)
	identity := sdd.RequestIdentity{Subject: "tester"}

	both, err := application.GroundingStatus(t.Context(), identity, gatherProject, sdd.GroundingStatusRequest{Verbose: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sources: 94 live labels, 95 replayed from past captures\n", "labels: 189 referenced", "thresholds: set aside below"} {
		if !strings.Contains(both.Text, want) {
			t.Errorf("status over both sources lacks %q:\n%s", want, both.Text)
		}
	}
	live, err := application.GroundingStatus(t.Context(), identity, gatherProject, sdd.GroundingStatusRequest{ExcludeReplay: true, Verbose: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"95 replayed from past captures; the report below reads the live labels alone", "labels: 94 referenced", "thresholds: none yet"} {
		if !strings.Contains(live.Text, want) {
			t.Errorf("status over live labels lacks %q:\n%s", want, live.Text)
		}
	}
}

func TestGroundingStatus_ShowsTheFlipRate(t *testing.T) {
	var batch model.RelevanceBatch
	for i := range 4 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-can", i), "20250601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderImplicit, Flipped: i == 0}})
	}
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	f := prefilterFixture(t, &scriptedScorer{})
	f.relevance = store
	status, err := f.app(t, prefilterPolicy(1), nil).GroundingStatus(t.Context(), sdd.RequestIdentity{Subject: "tester"}, gatherProject, sdd.GroundingStatusRequest{Verbose: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := "implicit dismissals: 4 recorded, 1 flipped to bearing (flip rate 25.0%)\n"; !strings.Contains(status.Text+"\n", want) {
		t.Fatalf("status should name the implicit dismissals recorded, those flipped and the flip rate:\n%s", status.Text)
	}
}

// The default status reports the one ranking judge of a command without a
// client and where it comes from; --verbose reports every judge.
func TestGroundingStatus_ReportsTheRankingJudgeAndItsOrigin(t *testing.T) {
	for _, tc := range []struct {
		name      string
		host      bool
		claudeCLI bool
		want      string
	}{
		{name: "Claude host default", host: true, claudeCLI: true, want: "judge:   " + rescoreHost + " (default on a Claude host; `llm_prefilter` would replace it)\n"},
		{name: "llm provider default", host: true, want: "judge:   " + gatherJudge + " (default on the `llm` provider; `llm_prefilter` would replace it)\n"},
		{name: "configured", want: "judge:   " + gatherJudge + " (set by `llm_prefilter`)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := prefilterFixture(t, &scriptedScorer{})
			f.relevance = localadapter.NewMemoryRelevanceStore()
			if tc.host {
				f.ranking = sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: &scriptedScorer{}, Judge: rescoreHost}, ClaudeCLI: tc.claudeCLI}
			}
			application := f.app(t, prefilterPolicy(1), nil)
			identity := sdd.RequestIdentity{Subject: "tester"}
			status, err := application.GroundingStatus(t.Context(), identity, gatherProject, sdd.GroundingStatusRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(status.Text, "labels:  0 pairs scored by people's choices\n") || !strings.Contains(status.Text+"\n", tc.want) {
				t.Fatalf("status should name the judge as %q:\n%s", tc.want, status.Text)
			}
			if tc.host {
				verbose, err := application.GroundingStatus(t.Context(), identity, gatherProject, sdd.GroundingStatusRequest{Verbose: true})
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(verbose.Text, "judge: "+rescoreHost+"\n") || !strings.Contains(verbose.Text, "judge: "+gatherJudge+"\n") {
					t.Fatalf("--verbose should report every judge:\n%s", verbose.Text)
				}
			}
		})
	}
}
