package llmops_test

import (
	"context"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/pkg/llm"
)

func TestGatherQueryWritesTheSearchesFromTheDraft(t *testing.T) {
	var got llm.Request
	runner := llm.RunnerFunc(func(_ context.Context, request llm.Request) (llm.Result, error) {
		got = request
		return llm.Result{Text: `{"query": "engine-enforced grounding", "terms": ["gather", " Gather ", "", "grounding.enforce"]}`}, nil
	})
	queries, err := llmops.GatherQuery(t.Context(), runner, llmops.GatherQueryDraft{Kind: "directive", Topics: []string{"reliability/grounding"}, Text: "Captures read the graph first."})
	if err != nil {
		t.Fatal(err)
	}
	if got.Purpose != llm.PurposeGatherQuery || len(got.ResponseSchema) == 0 {
		t.Errorf("the request should carry the gather-query purpose and its response schema: %+v", got)
	}
	for _, want := range []string{"Captures read the graph first.", "directive", "reliability/grounding"} {
		if !strings.Contains(got.UserPrompt, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, got.UserPrompt)
		}
	}
	if queries.Query != "engine-enforced grounding" || strings.Join(queries.Terms, "|") != "gather|grounding.enforce" {
		t.Errorf("queries should be trimmed and deduplicated: %+v", queries)
	}
}

func TestGatherQueryRefusesAnEmptyAnswer(t *testing.T) {
	for name, answer := range map[string]string{
		"no query": `{"query": " ", "terms": ["gather"]}`,
		"no term":  `{"query": "grounding", "terms": [" "]}`,
		"no json":  `the queries are grounding and gather`,
	} {
		t.Run(name, func(t *testing.T) {
			runner := llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) { return llm.Result{Text: answer}, nil })
			if _, err := llmops.GatherQuery(t.Context(), runner, llmops.GatherQueryDraft{Text: "A draft."}); err == nil {
				t.Fatal("an unusable answer was accepted")
			}
		})
	}
	if _, err := llmops.GatherQuery(t.Context(), nil, llmops.GatherQueryDraft{}); err == nil {
		t.Fatal("a draft without text was accepted")
	}
}
