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

// The rubric pre-flight hands the deep verifier carries the rules whose
// guard the draft meets and the active contracts, but neither the draft nor
// the entries it refs, closes or supersedes: the verifier's reading pages
// carry those. Pre-flight's own prompt keeps every rule and every entry
// (20261112-075604-d-tac-t4r).
func TestPreflightRubricLeavesTheReadingAndInapplicableRulesOut(t *testing.T) {
	facts, err := basefacts.Entries(viewlayout.Vocabulary{})
	if err != nil {
		t.Fatal(err)
	}
	plan := &model.Entry{ID: "20260101-000000-d-tac-pln", Type: model.TypeDecision, Kind: model.KindPlan, Layer: model.LayerTactical, Content: "The plan body.\n\n## Acceptance criteria\n\n- [ ] one"}
	directive := &model.Entry{ID: "20260101-000003-d-tac-dir", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Content: "The directive body."}
	fact := &model.Entry{ID: "20260101-000001-s-cpt-fct", Type: model.TypeSignal, Kind: model.KindFact, Layer: model.LayerConceptual, Content: "The fact body."}
	gap := &model.Entry{ID: "20260101-000002-s-tac-gap", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Content: "The gap body."}
	contract := &model.Entry{ID: "20260101-000004-d-cpt-con", Type: model.TypeDecision, Kind: model.KindContract, Layer: model.LayerConceptual, Content: "The contract body."}
	graph := model.NewGraph(append(facts, plan, directive, fact, gap, contract))
	reading := []string{"The plan body.", "The directive body.", "The fact body.", "The gap body."}

	const (
		acPresence    = "**Acceptance criteria presence**"
		directiveRule = "**Directive shape check**"
		directiveCal  = "### Directive-reads-aspiration-shaped calibration"
		retirement    = "**Retirement rationale**"
		retirementCal = "### Retirement-rationale calibration"
		acCoverage    = "**Acceptance criteria coverage**"
		acCoverageCal = "### AC coverage calibration"
		durability    = "## Artifact durability"
		unusualClose  = "## Unusual close-pattern check"
		refMeta       = "## Reference metadata consistency"
		unrelatedRefs = "## Unrelated references check"
		settled       = "## Settled-directive justification"
	)
	for _, tc := range []struct {
		name  string
		draft *model.Entry
		rules []string
		not   []string
	}{
		{
			name:  "conceptual directive with refs",
			draft: &model.Entry{Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerConceptual, Content: "Draft text one.", Refs: []model.Ref{{ID: gap.ID}}},
			rules: []string{directiveRule, directiveCal, refMeta, unrelatedRefs},
			not:   []string{acPresence, durability, unusualClose, settled},
		},
		{
			name:  "tactical plan with refs",
			draft: &model.Entry{Type: model.TypeDecision, Kind: model.KindPlan, Layer: model.LayerTactical, Content: "Draft text two.", Refs: []model.Ref{{ID: gap.ID}}},
			rules: []string{acPresence, refMeta},
			not:   []string{directiveRule, directiveCal, durability, unusualClose, settled},
		},
		{
			name:  "settled directive with refs",
			draft: &model.Entry{Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Intent: model.IntentSettled, Content: "Draft text three.", Refs: []model.Ref{{ID: gap.ID}}},
			rules: []string{settled},
			not:   []string{directiveRule, directiveCal, acPresence},
		},
		{
			name:  "directive retiring a fact",
			draft: &model.Entry{Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Content: "Draft text four.", Closes: []string{fact.ID}},
			rules: []string{retirement, retirementCal, unusualClose},
			not:   []string{refMeta, unrelatedRefs, durability},
		},
		{
			name:  "directive closing a gap",
			draft: &model.Entry{Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Content: "Draft text five.", Closes: []string{gap.ID}},
			not:   []string{retirement, retirementCal},
		},
		{
			name:  "done closing a plan",
			draft: &model.Entry{Type: model.TypeSignal, Kind: model.KindDone, Layer: model.LayerTactical, Content: "Draft text six.", Closes: []string{plan.ID}},
			rules: []string{acCoverage, acCoverageCal, durability},
		},
		{
			name:  "done closing a directive",
			draft: &model.Entry{Type: model.TypeSignal, Kind: model.KindDone, Layer: model.LayerTactical, Content: "Draft text seven.", Closes: []string{directive.ID}},
			rules: []string{durability},
			not:   []string{acCoverage, acCoverageCal},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req llm.Request
			runner := llm.RunnerFunc(func(_ context.Context, r llm.Request) (llm.Result, error) {
				req = r
				return llm.Result{Text: `{"findings":[]}`}, nil
			})
			_, err := llmops.Preflight(t.Context(), runner, tc.draft, graph, "")
			if err != nil {
				t.Fatal(err)
			}
			prompt := req.SystemPrompt + "\n\n" + req.UserPrompt
			rubric, err := llmops.PreflightRubric(tc.draft, graph, "")
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(prompt, tc.draft.Content) || strings.Contains(rubric, tc.draft.Content) {
				t.Errorf("the draft should reach pre-flight's prompt and stay out of the rubric")
			}
			for _, heading := range []string{"## Proposed ", "## Referenced entries", "## Entries being closed", "## Entry being closed"} {
				if strings.Contains(rubric, heading) {
					t.Errorf("the rubric should drop the section the reading pages carry: %q", heading)
				}
			}
			for _, body := range reading {
				if strings.Contains(rubric, body) {
					t.Errorf("the rubric should leave the reading's entries to the reading pages, found %q", body)
				}
			}
			if !strings.Contains(rubric, contract.Content) {
				t.Errorf("the rubric should keep the active contracts")
			}
			for _, rule := range tc.rules {
				if !strings.Contains(rubric, rule) || !strings.Contains(prompt, rule) {
					t.Errorf("a rule whose guard the draft meets should be in the rubric and the prompt: %q", rule)
				}
			}
			for _, rule := range tc.not {
				if strings.Contains(rubric, rule) {
					t.Errorf("a rule whose guard the draft fails should be left out of the rubric: %q", rule)
				}
			}
			for _, universal := range []string{durability, unusualClose, refMeta, unrelatedRefs, settled} {
				if !strings.Contains(req.SystemPrompt, universal) {
					t.Errorf("pre-flight's prompt should keep every universal rule for its model to guard: %q", universal)
				}
			}
		})
	}
}
