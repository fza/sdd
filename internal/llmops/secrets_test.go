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
	"github.com/networkteam/sdd/internal/viewlayout"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/test"
)

// Each operation marks its draft as new, so the provider guard refuses a draft
// holding a credential whatever called the operation.
func TestOperationsMarkTheDraftAsNew(t *testing.T) {
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/github-pat.txt")
	if err != nil {
		t.Fatal(err)
	}
	credential := strings.TrimSpace(string(content))
	facts, err := basefacts.Entries(viewlayout.Vocabulary{})
	if err != nil {
		t.Fatal(err)
	}
	graph := model.NewGraph(facts)
	entry := &model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerOperational, Content: "Setup:\n" + credential}
	operations := []struct {
		name  string
		where string
		run   func(llm.Runner) error
	}{
		{name: "pre-flight", where: "the body", run: func(r llm.Runner) error {
			_, err := llmops.Preflight(t.Context(), r, entry, graph, "", nil)
			return err
		}},
		{name: "light pre-flight", where: "the body", run: func(r llm.Runner) error {
			_, err := llmops.PreflightLight(t.Context(), r, entry, graph, "", llmops.LightReading{}, 100000, nil)
			return err
		}},
		{name: "description", where: "the body", run: func(r llm.Runner) error {
			_, err := llmops.Describe(t.Context(), r, entry, graph, "", false, nil)
			return err
		}},
		{name: "writing guide", where: "the body", run: func(r llm.Runner) error {
			_, err := llmops.WritingGuide(t.Context(), r, entry, nil, llmops.ReferenceFacts{
				Source: responseFactSource{graph}, TypeSystemFactID: basefacts.OverviewFactID,
			})
			return err
		}},
		{name: "relevance", where: "the draft", run: func(r llm.Runner) error {
			judge, err := llmops.NewChatJudge(r)
			if err != nil {
				return err
			}
			_, err = judge.Score(t.Context(), relevance.Request{Subject: entry.Content, Candidates: []relevance.Candidate{{ID: "a", Text: "A stored entry."}}})
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

			if !errors.Is(err, secrets.ErrCredential) || !strings.Contains(err.Error(), operation.where) {
				t.Errorf("the draft should be refused naming its part: %v", err)
			}
			if calls != 0 {
				t.Error("a refused draft must not reach the provider")
			}
		})
	}
}
