package llmops_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

func TestRelevanceBridgeRatesOneCandidatePerCall(t *testing.T) {
	var got llm.Request
	runner := llm.RunnerFunc(func(_ context.Context, request llm.Request) (llm.Result, error) {
		got = request
		return llm.Result{Text: `{"score": 0.8}`, Identity: llm.Identity{Model: "small"}}, nil
	})
	bridge, err := llmops.NewRelevanceBridge(runner)
	if err != nil {
		t.Fatal(err)
	}
	if bridge.MaxBatch() != 1 || bridge.Direction() != relevance.Both {
		t.Fatal("the bridge should rate one pair per call, reading both texts")
	}
	result, err := bridge.Score(t.Context(), relevance.Request{Subject: "The draft.", Candidates: []relevance.Candidate{{ID: "c", Text: "The candidate."}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Purpose != llm.PurposeRelevance || len(got.ResponseSchema) == 0 || !strings.Contains(got.UserPrompt, "The draft.") || !strings.Contains(got.UserPrompt, "The candidate.") {
		t.Fatalf("request %+v should carry the purpose, the schema and both texts", got)
	}
	if len(result.Scores) != 1 || result.Scores[0] != 0.8 || result.Identity.Model != "small" {
		t.Fatalf("result %+v should carry the score and identity", result)
	}
}

func TestRelevanceBridgeRefusesMalformedAnswers(t *testing.T) {
	for name, answer := range map[string]string{"no json": "relevant", "no score": `{"relevant": true}`, "out of range": `{"score": 2}`} {
		t.Run(name, func(t *testing.T) {
			runner := llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) { return llm.Result{Text: answer}, nil })
			bridge, err := llmops.NewRelevanceBridge(runner)
			if err != nil {
				t.Fatal(err)
			}
			_, err = bridge.Score(t.Context(), relevance.Request{Candidates: []relevance.Candidate{{ID: "c"}}})
			if _, ok := errors.AsType[*llm.MalformedAnswerError](err); !ok {
				t.Fatalf("error %v should report a malformed answer", err)
			}
		})
	}
}
