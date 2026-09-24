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

// observedMalformations are the response shapes checkers actually produced when
// a capture was lost. Each is pinned here so a rubric or parser change that
// would stop rescuing one fails a test rather than a capture.
var observedMalformations = []struct {
	name  string
	first string
}{
	{
		name:  "conclusion in the severity field",
		first: `{"findings":[{"observation":"The concern does not hold.","category":"ref-applicability","severity":"no finding"}]}`,
	},
	{
		name:  "none as a severity",
		first: `{"findings":[{"observation":"Nothing to report.","category":"type-correctness","severity":"none"}]}`,
	},
	{
		name:  "empty severity",
		first: `{"findings":[{"observation":"Reads fine.","category":"layer-appropriate","severity":""}]}`,
	},
	{
		name:  "empty category",
		first: `{"findings":[{"observation":"Reads fine.","category":"","severity":"low"}]}`,
	},
	{
		name:  "prose with no JSON at all",
		first: "I reviewed the entry and it looks fine to me.",
	},
	{
		name:  "one bad finding among good ones",
		first: `{"findings":[{"observation":"Layer fits.","category":"layer-appropriate","severity":"low"},{"observation":"Withdrawn.","category":"ref-applicability","severity":"no finding"}]}`,
	},
}

func TestPreflightRescuesEveryObservedMalformation(t *testing.T) {
	const extracted = `{"findings":[{"observation":"Layer fits.","category":"layer-appropriate","severity":"low"}]}`

	for _, tc := range observedMalformations {
		t.Run(tc.name, func(t *testing.T) {
			runner := &scriptedRunner{replies: []string{tc.first, extracted}}

			result, err := runPreflight(t, runner)
			if err != nil {
				t.Fatalf("the capture must survive this response: %v", err)
			}
			if len(result.Findings) != 1 || result.Findings[0].Category != "layer-appropriate" {
				t.Errorf("findings came from the wrong response: %+v", result.Findings)
			}
			if len(runner.requests) != 2 {
				t.Errorf("expected exactly one fallback call, got %d call(s)", len(runner.requests))
			}
		})
	}
}

// The fallback is a reformat, not a retry: a transport failure on the first
// call has nothing to reformat and must surface as itself.
func TestPreflightDoesNotFallBackOnATransportFailure(t *testing.T) {
	runner := &scriptedRunner{errs: []error{fmt.Errorf("gollm mistral: timed out")}}

	if _, err := runPreflight(t, runner); err == nil {
		t.Fatal("a transport failure must surface")
	} else if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should name the transport failure, got %v", err)
	}
	if len(runner.requests) != 1 {
		t.Errorf("a transport failure must not spend a second call, got %d", len(runner.requests))
	}
}
