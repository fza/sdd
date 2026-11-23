package llmops_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/testsupport"
	"github.com/networkteam/sdd/internal/viewlayout"
	"github.com/networkteam/sdd/pkg/llm"
)

// Each operation marks its draft as new, so the provider guard refuses a draft
// holding a credential whatever called the operation.
func TestOperationsMarkTheDraftAsNew(t *testing.T) {
	content, err := fs.ReadFile(testsupport.SecretFixtures(), "positive/github-pat.txt")
	if err != nil {
		t.Fatal(err)
	}
	facts, err := basefacts.Entries(viewlayout.Vocabulary{})
	if err != nil {
		t.Fatal(err)
	}
	graph := model.NewGraph(facts)
	entry := &model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerOperational, Content: "Setup:\n" + strings.TrimSpace(string(content))}
	operations := []struct {
		name string
		run  func(llm.Runner) error
	}{
		{name: "pre-flight", run: func(r llm.Runner) error {
			_, err := llmops.Preflight(t.Context(), r, entry, graph, "", nil)
			return err
		}},
		{name: "summary", run: func(r llm.Runner) error {
			_, err := llmops.Summarize(t.Context(), r, entry, graph, "")
			return err
		}},
		{name: "writing guide", run: func(r llm.Runner) error {
			_, err := llmops.WritingGuide(t.Context(), r, entry, nil, llmops.ReferenceFacts{
				Source: responseFactSource{graph}, TypeSystemFactID: basefacts.OverviewFactID,
			})
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			calls := 0
			guarded := secrets.GuardRunner(llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) {
				calls++
				return llm.Result{Text: `{"findings":[]}`}, nil
			}))

			err := operation.run(guarded)

			if !errors.Is(err, secrets.ErrCredential) || !strings.Contains(err.Error(), "the body") {
				t.Errorf("the draft should be refused naming its body: %v", err)
			}
			if calls != 0 {
				t.Error("a refused draft must not reach the provider")
			}
		})
	}
}
