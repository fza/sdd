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

	both, err := application.GroundingStatus(t.Context(), identity, gatherProject, sdd.GroundingStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sources: 94 live labels, 95 replayed from past captures\n", "labels: 189 referenced", "thresholds: set aside below"} {
		if !strings.Contains(both.Text, want) {
			t.Errorf("status over both sources lacks %q:\n%s", want, both.Text)
		}
	}
	live, err := application.GroundingStatus(t.Context(), identity, gatherProject, sdd.GroundingStatusRequest{ExcludeReplay: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"95 replayed from past captures; the report below reads the live labels alone", "labels: 94 referenced", "thresholds: none yet"} {
		if !strings.Contains(live.Text, want) {
			t.Errorf("status over live labels lacks %q:\n%s", want, live.Text)
		}
	}
}
