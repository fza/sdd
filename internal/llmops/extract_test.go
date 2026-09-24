package llmops_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/viewlayout"
	"github.com/networkteam/sdd/pkg/llm"
)

// scriptedRunner answers each call from a script and records what it was asked.
type scriptedRunner struct {
	replies  []string
	errs     []error
	requests []llm.Request
}

func (r *scriptedRunner) Run(_ context.Context, req llm.Request) (llm.Result, error) {
	r.requests = append(r.requests, req)
	i := len(r.requests) - 1
	if i < len(r.errs) && r.errs[i] != nil {
		return llm.Result{}, r.errs[i]
	}
	if i >= len(r.replies) {
		return llm.Result{}, fmt.Errorf("scripted runner exhausted after %d calls", len(r.replies))
	}
	return llm.Result{Text: r.replies[i], Identity: llm.Identity{Provider: "scripted", Model: "m"}}, nil
}

func TestPreflightExtractsAfterAnUnparseableSeverity(t *testing.T) {
	runner := &scriptedRunner{replies: []string{
		`{"findings":[{"observation":"The ref holds.","category":"ref-applicability","severity":"no finding"}]}`,
		`{"findings":[{"observation":"The ref holds.","category":"ref-applicability","severity":"low"}]}`,
	}}

	result, err := runPreflight(t, runner)
	if err != nil {
		t.Fatalf("a severity outside the set must not lose the capture: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Severity != llmops.SeverityLow {
		t.Fatalf("findings came from the wrong response: %+v", result.Findings)
	}
	if len(runner.requests) != 2 {
		t.Fatalf("expected a fallback call, got %d call(s)", len(runner.requests))
	}

	extract := runner.requests[1]
	if extract.Purpose != llm.PurposePreflightExtract {
		t.Errorf("extraction purpose = %q, want %q", extract.Purpose, llm.PurposePreflightExtract)
	}
	if !strings.Contains(extract.UserPrompt, "no finding") {
		t.Error("the extraction call must carry the unparseable output verbatim")
	}
	if !strings.Contains(extract.SystemPrompt, "high") || !strings.Contains(extract.SystemPrompt, "medium") {
		t.Error("the extraction call must carry the severity scale")
	}
	if strings.Contains(extract.SystemPrompt, "Calibrating against active contracts") {
		t.Error("the extraction call must not carry the task rubric: it reformats, it does not judge")
	}
	if words := len(strings.Fields(extract.SystemPrompt)); words > 160 {
		t.Errorf("extraction prompt is %d words, which is more than a reformat instruction needs", words)
	}
}

func TestPreflightMakesOneCallWhenTheFirstResponseParses(t *testing.T) {
	runner := &scriptedRunner{replies: []string{`{"findings":[]}`}}

	if _, err := runPreflight(t, runner); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("a parsing response must cost one call, got %d", len(runner.requests))
	}
}

func TestPreflightReportsBothPhasesWhenNeitherParses(t *testing.T) {
	runner := &scriptedRunner{replies: []string{
		`{"findings":[{"observation":"x","category":"c","severity":"no finding"}]}`,
		`{"findings":[{"observation":"x","category":"c","severity":"none"}]}`,
	}}

	_, err := runPreflight(t, runner)
	if err == nil {
		t.Fatal("a checker that cannot produce its own output shape must fail loudly")
	}
	if !strings.Contains(err.Error(), "no finding") || !strings.Contains(err.Error(), "none") {
		t.Errorf("the error must name both attempts, got %v", err)
	}
}

func runPreflight(t *testing.T, runner llm.Runner) (*llmops.PreflightResult, error) {
	t.Helper()
	graph := graphWithBaseFacts(t)
	entry := &model.Entry{
		ID:           "20260924-120000-s-tac-aaa",
		Type:         model.TypeSignal,
		Layer:        model.LayerTactical,
		Kind:         model.KindGap,
		Participants: []string{"Felix Zandanel"},
		Content:      "A gap worth naming.",
	}
	return llmops.Preflight(context.Background(), runner, entry, graph, "")
}

func graphWithBaseFacts(t *testing.T) *model.Graph {
	t.Helper()
	entries, err := basefacts.Entries(viewlayout.Vocabulary{})
	if err != nil {
		t.Fatalf("basefacts.Entries: %v", err)
	}
	return model.NewGraph(entries)
}
