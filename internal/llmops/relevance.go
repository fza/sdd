package llmops

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

//go:embed relevance_templates/*.tmpl
var relevanceTemplates embed.FS

var relevanceTemplate = template.Must(template.New("relevance").ParseFS(relevanceTemplates, "relevance_templates/*.tmpl"))

// RelevanceBridge is the prefilter's generate protocol
// (20261109-004506-d-cpt-udz, D23): a chat model rates one candidate per
// call, as the reference implementation measured that form's recall. Wrap
// it in relevance.PerItem to score a batch.
type RelevanceBridge struct {
	runner llm.Runner
	system string
}

// NewRelevanceBridge returns the bridge over a composed chat runner.
func NewRelevanceBridge(runner llm.Runner) (*RelevanceBridge, error) {
	var sys strings.Builder
	if err := relevanceTemplate.ExecuteTemplate(&sys, "relevance_system", nil); err != nil {
		return nil, fmt.Errorf("rendering the relevance prompt: %w", err)
	}
	return &RelevanceBridge{runner: runner, system: strings.TrimSpace(sys.String())}, nil
}

func (b *RelevanceBridge) Direction() relevance.Direction { return relevance.Both }
func (b *RelevanceBridge) MaxBatch() int                  { return 1 }
func (b *RelevanceBridge) MaxInputTokens() int            { return 0 }
func (b *RelevanceBridge) MaxPairTokens() int             { return 0 }

// Score rates the one candidate of request.
func (b *RelevanceBridge) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	if len(request.Candidates) != 1 {
		return relevance.Result{}, fmt.Errorf("the relevance bridge rates one candidate per call, got %d", len(request.Candidates))
	}
	var user strings.Builder
	data := struct{ Subject, Candidate string }{Subject: request.Subject, Candidate: request.Candidates[0].Text}
	if err := relevanceTemplate.ExecuteTemplate(&user, "relevance_user", data); err != nil {
		return relevance.Result{}, fmt.Errorf("rendering the relevance prompt: %w", err)
	}
	result, err := b.runner.Run(ctx, llm.Request{
		Purpose: llm.PurposeRelevance, SystemPrompt: b.system, UserPrompt: strings.TrimSpace(user.String()), ResponseSchema: relevanceSchema,
	})
	if err != nil {
		return relevance.Result{}, err
	}
	text, err := extractJSONObject(result.Text)
	if err != nil {
		return relevance.Result{}, &llm.Error{Identity: result.Identity, Usage: result.Usage, Err: &llm.MalformedAnswerError{Text: result.Text, Err: fmt.Errorf("parsing the relevance score: %w", err)}}
	}
	var answer struct {
		Score *float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(text), &answer); err != nil || answer.Score == nil || *answer.Score < 0 || *answer.Score > 1 {
		return relevance.Result{}, &llm.Error{Identity: result.Identity, Usage: result.Usage, Err: &llm.MalformedAnswerError{Text: result.Text, Err: fmt.Errorf("the relevance answer holds no score between 0 and 1")}}
	}
	return relevance.Result{Scores: []float64{*answer.Score}, Identity: result.Identity, Usage: result.Usage}, nil
}
