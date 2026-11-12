package llmops_test

import (
	"context"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

func answering(text string) llm.Runner {
	return llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) {
		return llm.Result{Text: text}, nil
	})
}

func quoteGraph() (*model.Entry, *model.Graph) {
	fact := &model.Entry{ID: basefacts.RefKindsFactID, Type: model.TypeSignal, Layer: model.LayerProcess, Kind: model.KindFact, Content: "ref-kind vocabulary stub"}
	proposed := &model.Entry{Type: model.TypeSignal, Layer: model.LayerTactical, Content: "The release procedure gains a fixed section of its notes template."}
	return proposed, model.NewGraph([]*model.Entry{fact})
}

func TestPreflightFlagsAFindingWhoseQuoteLocatesNothing(t *testing.T) {
	tests := []struct {
		name         string
		quote        string
		wantSeverity llmops.Severity
		wantFlagged  bool
	}{
		{"a quote of the draft stands", "gains a fixed section … notes template", llmops.SeverityHigh, false},
		{"an invented quote is capped and flagged", "the Storybook proof requires worktime reasoning", llmops.SeverityMedium, true},
		{"an empty quote is capped and flagged", "", llmops.SeverityMedium, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposed, graph := quoteGraph()
			answer := `{"findings":[{"observation":"The section lacks an owner.","quote":"` + tt.quote + `","category":"scope","severity":"high"}]}`

			result, err := llmops.Preflight(context.Background(), answering(answer), proposed, graph, "", nil)
			if err != nil {
				t.Fatalf("Preflight: %v", err)
			}
			if len(result.Findings) != 1 {
				t.Fatalf("the finding should be kept, got %+v", result.Findings)
			}
			finding := result.Findings[0]
			if finding.Severity != tt.wantSeverity {
				t.Errorf("severity should follow whether the quote locates, got %s", finding.Severity)
			}
			if got := strings.HasPrefix(finding.Observation, llmops.UnlocatedNote); got != tt.wantFlagged {
				t.Errorf("the observation should carry the unverified note only for an unlocated quote, got %q", finding.Observation)
			}
			if result.HasBlocking() == tt.wantFlagged {
				t.Errorf("only a located high finding should block")
			}
		})
	}
}

func TestPreflightLightFlagsAFindingWhoseQuoteLocatesNothing(t *testing.T) {
	tests := []struct {
		name        string
		quote       string
		wantFlagged bool
	}{
		{"a quote of the draft blocks", "gains a fixed section", false},
		{"an invented quote is flagged below blocking", "Canada-Family Conversation Storybook", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposed, graph := quoteGraph()
			answer := `{"findings":[{"observation":"Duplicates a live entry.","quote":"` + tt.quote + `","basis":[],"category":"duplicates","severity":"high"}]}`

			result, err := llmops.PreflightLight(context.Background(), answering(answer), proposed, graph, "", llmops.LightReading{}, 1_000_000, nil)
			if err != nil {
				t.Fatalf("PreflightLight: %v", err)
			}
			if len(result.Findings) != 1 {
				t.Fatalf("the finding should be kept, got %+v", result.Findings)
			}
			if result.HasBlocking() == tt.wantFlagged || strings.HasPrefix(result.Findings[0].Observation, llmops.UnlocatedNote) != tt.wantFlagged {
				t.Errorf("only an unlocated light finding should be flagged below blocking, got %+v", result.Findings)
			}
		})
	}
}
