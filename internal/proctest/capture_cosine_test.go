package proctest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm/embed"
	"github.com/networkteam/sdd/pkg/local"
)

// directiveEmbedder places texts about the directive apart from all others.
var directiveEmbedder = embed.EmbedderFunc{Space: "proctest/directive", Run: func(_ context.Context, req embed.Request) (embed.Result, error) {
	vectors := make([][]float32, len(req.Texts))
	for i, text := range req.Texts {
		vectors[i] = []float32{0, 0, 0.1}
		if strings.Contains(strings.ToLower(text), "directive") {
			vectors[i][0] = 1
		}
	}
	return embed.Result{Vectors: vectors}, nil
}}

// The cosine stage's similarities ride the capture's reading to publish,
// which keeps them only for the pairs the reader labelled: a candidate the
// capture left unanswered stores nothing (5.8 item 11).
func TestCaptureGrounding_CosineSimilaritiesReachTheLedgerOnlyForLabelledPairs(t *testing.T) {
	store := local.NewMemoryRelevanceStore()
	world, session := newCaptureWorld(t, "grounding-cosine",
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, BriefAllBelow: 1, Prefilter: true, PrefilterFrom: 1}),
		proctest.WithRelevance(store), proctest.WithEmbedder(directiveEmbedder))
	world.LLM.GatherQuery, world.LLM.GatherTerm = "the fixture directive", "directive"
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, map[string]any{"body": "A tactical gap about the directive the fixture refs.", "entryKind": "gap", "topics": []any{"cli/ux"}})
	gathered, err := session.WF.Gather(t.Context(), world.Identity, sdd.WorkflowGatherRequest{Instance: instance, SyncMode: sdd.SearchSyncAll})
	if err != nil {
		t.Fatal(err)
	}
	const unanswered = "20260703-190000-d-prc-eng"
	if !strings.Contains(gathered.Text, unanswered) {
		t.Fatalf("the engage procedure should be a candidate the capture leaves unanswered:\n%s", gathered.Text)
	}
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	entry := confirmCapture(t, world, session, instance)

	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	judge := model.CosineJudge("proctest/directive")
	if _, ok := ledger.Score(model.NewRelevancePair(entry.ID, captureRefID), entry.ID, judge); !ok {
		t.Fatal("the referenced pair should keep its similarity for calibration")
	}
	if _, ok := ledger.Score(model.NewRelevancePair(entry.ID, unanswered), entry.ID, judge); ok {
		t.Fatal("a candidate the capture left unanswered should store no similarity")
	}
}
