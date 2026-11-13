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

// The reranker prefilter is gather's one judge: labels a ledger holds under
// a retired vector-similarity judge stay readable and set nothing aside, and
// the embedder serves the searches alone.
func TestGather_StoredSimilarityLabelsSetNothingAside(t *testing.T) {
	var batch model.RelevanceBatch
	for i := range 189 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-pos", i), "20250601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: "cosine:counter/v1", Score: 200}})
	}
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	f := newGatherFixture(t)
	f.seed(t)
	f.relevance = store
	embeddings := &countingEmbeddings{}
	application := f.app(t, sdd.GroundingPolicy{BriefAllBelow: gatherSearchSize, Prefilter: true, PrefilterFrom: 1}, embeddings)
	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha.", Kind: "gap", Topics: []string{"cli/ux"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text, "cosine") {
		t.Fatalf("no similarity stage should set a candidate aside:\n%s", result.Text)
	}
	for _, id := range []string{gatherAlpha, gatherSuccessor, gatherTopicOnly} {
		if lineOf(result.Text, id) == "" {
			t.Fatalf("candidate %s should stay to judge:\n%s", id, result.Text)
		}
	}
}
