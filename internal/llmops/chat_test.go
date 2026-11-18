package llmops_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// The chat judge scores up to 100 candidates in one call: each listed as
// its label and one line, entry IDs stripped from the lines and the draft,
// the answer held to an object keyed by every label and no other.
func TestChatJudgeScoresAListInOneCall(t *testing.T) {
	var got llm.Request
	runner := llm.RunnerFunc(func(_ context.Context, request llm.Request) (llm.Result, error) {
		got = request
		return llm.Result{Text: `{"scores": {"c2": 0.1, "c1": 0.9, "c3": 0}}`, Identity: llm.Identity{Model: "haiku"}}, nil
	})
	judge, err := llmops.NewChatJudge(runner)
	if err != nil {
		t.Fatal(err)
	}
	if judge.MaxBatch() != llmops.ChatBatch || llmops.ChatBatch != 100 || judge.Direction() != relevance.Both {
		t.Fatal("the judge should take up to 100 candidates a call, reading both texts")
	}
	result, err := judge.Score(t.Context(), relevance.Request{
		Subject: "The draft refines 20260101-000000-d-cpt-abc (see 20260101-000000-d-cpt-abc).",
		Candidates: []relevance.Candidate{
			{ID: "a", Text: "full a", Line: "[decision/directive] First: rests on 20260102-000000-s-tac-xyz."},
			{ID: "b", Text: "[signal/gap] Second: no line given."},
			{ID: "c", Line: "[signal/gap] Third: unrelated."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Scores, []float64{0.9, 0.1, 0}) || result.Identity.Model != "haiku" {
		t.Fatalf("result %+v: scores should follow the candidates' order", result)
	}
	if got.Purpose != llm.PurposeRelevance || !strings.Contains(got.SystemPrompt, "Rate every candidate") {
		t.Fatalf("request %+v should carry the relevance purpose and the chat prompt", got)
	}
	for _, want := range []string{"The draft refines", "c1 [decision/directive] First: rests on .", "c2 [signal/gap] Second: no line given.", "c3 [signal/gap] Third: unrelated."} {
		if !strings.Contains(got.UserPrompt, want) {
			t.Errorf("user prompt lacks %q:\n%s", want, got.UserPrompt)
		}
	}
	if strings.Contains(got.UserPrompt, "20260101-000000-d-cpt-abc") || strings.Contains(got.UserPrompt, "20260102-000000-s-tac-xyz") || strings.Contains(got.UserPrompt, "()") {
		t.Fatalf("entry IDs and the brackets they leave should be stripped:\n%s", got.UserPrompt)
	}
	var schema struct {
		Properties struct {
			Scores struct {
				Properties           map[string]map[string]string `json:"properties"`
				Required             []string                     `json:"required"`
				AdditionalProperties *bool                        `json:"additionalProperties"`
			} `json:"scores"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(got.ResponseSchema, &schema); err != nil {
		t.Fatal(err)
	}
	scores := schema.Properties.Scores
	if !slices.Equal(scores.Required, []string{"c1", "c2", "c3"}) || len(scores.Properties) != 3 || scores.Properties["c3"]["type"] != "number" ||
		scores.AdditionalProperties == nil || *scores.AdditionalProperties || !slices.Equal(schema.Required, []string{"scores"}) {
		t.Fatalf("schema %s should require a number for every label and allow no other key", got.ResponseSchema)
	}
}

func TestChatJudgeRefusesMalformedAnswers(t *testing.T) {
	for name, answer := range map[string]string{
		"no json":       "relevant",
		"no scores":     `{"relevant": true}`,
		"missing label": `{"scores": {"c1": 0.5}}`,
		"extra label":   `{"scores": {"c1": 0.5, "c2": 0.5, "c3": 0.5}}`,
		"out of range":  `{"scores": {"c1": 0.5, "c2": 2}}`,
	} {
		t.Run(name, func(t *testing.T) {
			runner := llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) { return llm.Result{Text: answer}, nil })
			judge, err := llmops.NewChatJudge(runner)
			if err != nil {
				t.Fatal(err)
			}
			_, err = judge.Score(t.Context(), relevance.Request{Candidates: []relevance.Candidate{{ID: "a", Line: "a"}, {ID: "b", Line: "b"}}})
			if _, ok := errors.AsType[*llm.MalformedAnswerError](err); !ok {
				t.Fatalf("error %v should report a malformed answer", err)
			}
		})
	}
}

func TestChatJudgeRefusesMoreThanABatch(t *testing.T) {
	runner := llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) {
		t.Fatal("an oversize request should not reach the model")
		return llm.Result{}, nil
	})
	judge, err := llmops.NewChatJudge(runner)
	if err != nil {
		t.Fatal(err)
	}
	var candidates []relevance.Candidate
	for i := range llmops.ChatBatch + 1 {
		candidates = append(candidates, relevance.Candidate{ID: fmt.Sprint(i), Line: "x"})
	}
	if _, err := judge.Score(t.Context(), relevance.Request{Candidates: candidates}); err == nil {
		t.Fatal("more candidates than a batch should be refused")
	}
}
