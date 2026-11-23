package llmops_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

func answeringKindFit(text string) llm.Runner {
	return llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) {
		return llm.Result{Text: text}, nil
	})
}

func kindFitDone() *model.KindFit {
	fit := &model.KindFit{Kind: model.KindDone, Question: basefacts.KindQuestion(model.KindDone)}
	for _, k := range []model.Kind{model.KindGap, model.KindDirective, model.KindPlan} {
		fit.Stricter = append(fit.Stricter, model.KindTest{Kind: k, Question: basefacts.KindQuestion(k)})
	}
	return fit
}

func doneDraft() (*model.Entry, *model.Graph) {
	fact := &model.Entry{ID: basefacts.RefKindsFactID, Type: model.TypeSignal, Layer: model.LayerProcess, Kind: model.KindFact, Content: "ref-kind vocabulary stub"}
	draft := &model.Entry{
		Type:    model.TypeSignal,
		Layer:   model.LayerTactical,
		Kind:    model.KindDone,
		Content: "Waiting instruction lanes now drain however a follow-up call proves or rereads, fixed on `main`. An agent sending wrong held proofs or rereads looped 101 calls without a report at capture assemble.",
	}
	return draft, model.NewGraph([]*model.Entry{fact})
}

func kindFitAnswer(t *testing.T, category, observation string, light bool) string {
	t.Helper()
	finding := map[string]any{"observation": observation, "category": category, "severity": "high"}
	if light {
		finding["basis"] = []string{}
	} else {
		// A pre-flight finding whose quote locates nothing does not block.
		finding["quote"] = "Waiting instruction lanes now drain"
	}
	answer, err := json.Marshal(map[string]any{"findings": []any{finding}})
	if err != nil {
		t.Fatal(err)
	}
	return string(answer)
}

func TestPreflightKindFitFindingBlocksOnlyWhenItNamesAnotherKind(t *testing.T) {
	tests := []struct {
		name         string
		category     string
		observation  string
		wantBlocking bool
	}{
		{
			name:         "a kind-fit finding affirming the chosen kind does not block",
			category:     "kind-fit",
			observation:  "The draft's body records completed work: it names the fix, the loop it removes, and the narrowed reread rule, answering the done question ('What was accomplished?'). It commits to no direction and does not prescribe next steps. Kind `done` fits.",
			wantBlocking: false,
		},
		{
			name:         "a kind-fit finding naming a stricter kind blocks",
			category:     "kind-fit",
			observation:  "The body commits the team to how lanes serve from now on, answering what was decided. Kind `directive` fits, not `done`.",
			wantBlocking: true,
		},
		{
			name:         "another category naming no kind still blocks",
			category:     "durability",
			observation:  "The body names a commit hash as its only evidence.",
			wantBlocking: true,
		},
	}
	runs := []struct {
		name  string
		light bool
		run   func(answer string, draft *model.Entry, graph *model.Graph) (*llmops.PreflightResult, error)
	}{
		{"pre-flight", false, func(answer string, draft *model.Entry, graph *model.Graph) (*llmops.PreflightResult, error) {
			return llmops.Preflight(context.Background(), answeringKindFit(answer), draft, graph, "", kindFitDone())
		}},
		{"light", true, func(answer string, draft *model.Entry, graph *model.Graph) (*llmops.PreflightResult, error) {
			return llmops.PreflightLight(context.Background(), answeringKindFit(answer), draft, graph, "", llmops.LightReading{}, 1_000_000, kindFitDone())
		}},
	}
	for _, run := range runs {
		for _, tt := range tests {
			t.Run(run.name+"/"+tt.name, func(t *testing.T) {
				draft, graph := doneDraft()

				result, err := run.run(kindFitAnswer(t, tt.category, tt.observation, run.light), draft, graph)
				if err != nil {
					t.Fatalf("pre-flight: %v", err)
				}
				if len(result.Findings) != 1 {
					t.Fatalf("the finding should be kept, got %+v", result.Findings)
				}
				if got := result.HasBlocking(); got != tt.wantBlocking {
					t.Errorf("blocking = %v, want %v: %+v", got, tt.wantBlocking, result.Findings)
				}
				if got := strings.HasPrefix(result.Findings[0].Observation, llmops.KindFitUnnamedNote); got == tt.wantBlocking {
					t.Errorf("only a kind-fit finding naming no other kind should carry the note, got %q", result.Findings[0].Observation)
				}
			})
		}
	}
}
