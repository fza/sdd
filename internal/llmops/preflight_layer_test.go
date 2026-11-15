package llmops_test

import (
	"context"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/viewlayout"
	"github.com/networkteam/sdd/pkg/llm"
)

// Pre-flight checks the layer of every signal and decision both ways, by the
// layer meanings the capture serves.
func TestPreflightChecksTheLayerByTheModelMeanings(t *testing.T) {
	facts, err := basefacts.Entries(viewlayout.Vocabulary{})
	if err != nil {
		t.Fatal(err)
	}
	gap := &model.Entry{ID: "20260101-000002-s-tac-gap", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Content: "The gap body."}
	graph := model.NewGraph(append(facts, gap))
	for _, tc := range []struct {
		name  string
		draft *model.Entry
	}{
		{"gap", &model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerOperational, Content: "The loader drops the second option."}},
		{"directive without refs", &model.Entry{Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerOperational, Content: "The parser rejects empty input."}},
		{"plan with refs", &model.Entry{Type: model.TypeDecision, Kind: model.KindPlan, Layer: model.LayerTactical, Content: "Rename the flag.", Refs: []model.Ref{{ID: gap.ID}}}},
		{"directive closing a gap", &model.Entry{Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Content: "Bump the dependency.", Closes: []string{gap.ID}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req llm.Request
			runner := llm.RunnerFunc(func(_ context.Context, r llm.Request) (llm.Result, error) {
				req = r
				return llm.Result{Text: `{"findings":[]}`}, nil
			})
			_, err := llmops.Preflight(t.Context(), runner, tc.draft, graph, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			prompt := req.SystemPrompt + "\n\n" + req.UserPrompt
			for _, want := range []string{"**Layer appropriateness**", model.LayerGuide(), "belongs at `tactical`, not `operational`", "belongs at `operational`, not `tactical`"} {
				if !strings.Contains(prompt, want) {
					t.Errorf("the prompt should check the layer both ways by the model's meanings, missing %q", want)
				}
			}
		})
	}
}
