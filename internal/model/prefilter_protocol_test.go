package model_test

import (
	"slices"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// Config set and validation name only the current protocols.
func TestPrefilterProtocol_AllowsOnlyTheCurrentValues(t *testing.T) {
	got := model.PrefilterProtocol("").AllowedValues()
	if want := []string{"systemone", "cohere", "chat"}; !slices.Equal(got, want) {
		t.Fatalf("AllowedValues = %v, want %v", got, want)
	}
}

// A config a former sdd wrote reads its protocol as the one it became.
func TestPrefilterProtocol_ReadsLegacyValuesAsCurrent(t *testing.T) {
	for legacy, want := range map[string]model.PrefilterProtocol{
		"generate":      model.PrefilterChat,
		"listwise":      model.PrefilterChat,
		"cohere-rerank": model.PrefilterCohere,
		"systemone":     model.PrefilterSystemOne,
		"chat":          model.PrefilterChat,
	} {
		t.Run(legacy, func(t *testing.T) {
			var cfg model.BaseConfig
			if err := model.UnmarshalYAML([]byte("llm_prefilter:\n  protocol: "+legacy+"\n"), &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.LLMPrefilter.Protocol != want {
				t.Fatalf("protocol %s read as %q, want %q", legacy, cfg.LLMPrefilter.Protocol, want)
			}
		})
	}
}

func TestCanonicalJudge(t *testing.T) {
	for stored, want := range map[string]string{
		"generate:gpt-5.5":          "chat:gpt-5.5",
		"listwise:claude-haiku-5-5": "chat:claude-haiku-5-5",
		"cohere-rerank:rerank-v4":   "cohere:rerank-v4",
		"systemone:jev-1.14.0":      "systemone:jev-1.14.0",
		"chat:claude-haiku-5-5":     "chat:claude-haiku-5-5",
		"generate":                  "generate",
	} {
		if got := model.CanonicalJudge(stored); got != want {
			t.Errorf("CanonicalJudge(%q) = %q, want %q", stored, got, want)
		}
	}
}

// Scores and drift steps stored under a legacy judge name keep counting for
// the judge it became.
func TestRelevanceLedger_CountsLegacyJudgeNamesUnderTheCurrentOne(t *testing.T) {
	pair := model.NewRelevancePair("20260101-000000-s-ops-aaa", "20260102-000000-s-ops-bbb")
	at := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	ledger := model.NewRelevanceLedger(model.RelevanceBatch{
		Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: "listwise:claude-haiku-5-5", Score: 40}}},
		Steps:  []model.DriftStep{{Judge: "cohere-rerank:rerank-v4", At: at, SetAside: 9}},
	})
	if score, ok := ledger.Score(pair, pair.B, "chat:claude-haiku-5-5"); !ok || score != 40 {
		t.Errorf("the listwise score should count for chat, got %d, %v", score, ok)
	}
	steps := ledger.Steps()
	if len(steps) != 1 || steps[0].Judge != "cohere:rerank-v4" {
		t.Errorf("the drift step should count for cohere, got %+v", steps)
	}
}

// A reading's scores stored under a legacy judge name answer the current one.
func TestReading_ScoresUnderALegacyJudgeName(t *testing.T) {
	var reading model.Reading
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{Judge: "generate:gpt-5.5", Subject: "s", Scores: map[string]uint8{"x": 7}}})
	scores := reading.Kept("chat:gpt-5.5")
	if scores["x"] != 7 {
		t.Fatalf("the generate scores should answer chat, got %v", scores)
	}
}
