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

// cosineLedger holds positive labels whose stored similarity under the
// counter embedder's cosine judge is score, enough for the recall bound.
func cosineLedger(t *testing.T, positives int, score uint8) *localadapter.MemoryRelevanceStore {
	t.Helper()
	var batch model.RelevanceBatch
	for i := range positives {
		pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-pos", i), "20250601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: model.CosineJudge("counter/v1"), Score: score}})
	}
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	return store
}

// Once the labelled pairs' similarities hold the recall bound, the cosine
// stage sets aside the candidates below the calibrated floor; before that it
// sets nothing aside (5.8 item 11).
func TestGather_CosineStageSetsAsideBelowItsCalibratedFloor(t *testing.T) {
	cases := []struct {
		name      string
		positives int
		setAside  bool
	}{
		{"calibrated", 189, true},
		{"too few labels", 188, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newGatherFixture(t)
			f.seed(t)
			f.relevance = cosineLedger(t, tc.positives, 200)
			application := f.app(t, sdd.GroundingPolicy{BriefAllBelow: gatherSearchSize, Prefilter: true, PrefilterFrom: 1}, &countingEmbeddings{})
			result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha.", Kind: "gap", Topics: []string{"cli/ux"}})
			if err != nil {
				t.Fatal(err)
			}
			line := setAsideLine(result.Text, model.SetAsideCosine)
			if (line != "") != tc.setAside {
				t.Fatalf("cosine set-aside line %q:\n%s", line, result.Text)
			}
			if !tc.setAside {
				return
			}
			if !strings.Contains(line, gatherTopicOnly) || strings.Contains(line, gatherAlpha) || strings.Contains(line, gatherSuccessor) {
				t.Fatalf("the dissimilar candidates should be set aside, the similar one kept:\n%s", result.Text)
			}
			if lineOf(result.Text, gatherAlpha) == "" || lineOf(result.Text, gatherSuccessor) == "" {
				t.Fatalf("the similar candidates should stay to judge:\n%s", result.Text)
			}
		})
	}
}
