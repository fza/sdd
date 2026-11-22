package llmops_test

import (
	"context"
	"slices"
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
// carry those. Pre-flight's own prompt keeps every rule and every entry.
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
			rules: []string{refMeta},
			not:   []string{acPresence, directiveRule, directiveCal, durability, unusualClose, settled},
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
			rules: []string{retirement, retirementCal},
			not:   []string{unusualClose, refMeta, unrelatedRefs, durability},
		},
		{
			name:  "gap closing a gap",
			draft: &model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Content: "Draft text eight.", Closes: []string{gap.ID}},
			rules: []string{unusualClose},
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
			_, err := llmops.Preflight(t.Context(), runner, tc.draft, graph, "", nil)
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

// The rubric leaves out what only pre-flight's model reads: its role, the
// layout of its prompt, its answer format, its severity scale and its reading.
// Every rule, calibration and active contract stays; pre-flight's own prompt
// keeps everything.
func TestPreflightRubricLeavesPreflightOnlySectionsOut(t *testing.T) {
	facts, err := basefacts.Entries(viewlayout.Vocabulary{})
	if err != nil {
		t.Fatal(err)
	}
	gap := &model.Entry{ID: "20260101-000002-s-tac-gap", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Content: "The gap body."}
	contract := &model.Entry{ID: "20260101-000004-d-cpt-con", Type: model.TypeDecision, Kind: model.KindContract, Layer: model.LayerConceptual, Content: "The contract body."}
	graph := model.NewGraph(append(facts, gap, contract))

	preflightOnly := []string{
		"You are a pre-flight validator",
		"User message carries check-specific task",
		"## Findings\n",
		"**Reasoning first, verdict last.**",
		"## Severity\n",
		"Before writing this draft, its author read the graph.",
		"Each finding also carries `basis`",
	}
	ownPrompt := []string{preflightOnly[0], preflightOnly[2], preflightOnly[3], preflightOnly[4]}
	kept := []string{
		"## Calibrating against active contracts",
		"## Dialogue context is grounding, not argument",
		"## Immutability as calibration context",
		"## Graph fit against the grounding reading",
		"`duplicates`", "`outdated`", "`strain`", "`dismissal`",
		"Severity on these axes:",
	}
	for _, tc := range []struct {
		name   string
		draft  *model.Entry
		prompt []string
		kept   []string
	}{
		{
			name:   "done with refs",
			draft:  &model.Entry{Type: model.TypeSignal, Kind: model.KindDone, Layer: model.LayerTactical, Content: "Draft done.", Refs: []model.Ref{{ID: gap.ID}}},
			prompt: append([]string{preflightOnly[1]}, ownPrompt...),
			kept:   append([]string{contract.Content, "## Artifact durability"}, kept...),
		},
		{
			name:   "annotation",
			draft:  &model.Entry{Type: model.TypeSignal, Kind: model.KindAnnotation, Layer: model.LayerTactical, Content: "Draft annotation.", Refs: []model.Ref{{ID: gap.ID}}},
			prompt: ownPrompt,
			kept:   append([]string{"## Annotation rubric"}, kept...),
		},
		{
			name:   "focus",
			draft:  &model.Entry{Type: model.TypeDecision, Kind: model.KindFocus, Layer: model.LayerTactical, Content: "Draft focus."},
			prompt: ownPrompt,
			kept:   append([]string{"## What this rubric does NOT enforce"}, kept...),
		},
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
			rubric, err := llmops.PreflightRubric(tc.draft, graph, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, section := range tc.prompt {
				if !strings.Contains(req.SystemPrompt, section) {
					t.Errorf("pre-flight's prompt should keep what its model reads: %q", section)
				}
			}
			for _, section := range preflightOnly {
				if strings.Contains(rubric, section) {
					t.Errorf("the rubric should leave out what only pre-flight's model reads: %q", section)
				}
			}
			for _, section := range tc.kept {
				if !strings.Contains(rubric, section) {
					t.Errorf("the rubric should keep every rule, calibration and contract: %q", section)
				}
			}
		})
	}
}

// The rules text leaves out what code decides before the verifier reads
// (20261122-101625-d-tac-r0s): a plan's acceptance-criteria rule, the
// applicability paragraph, the close-pattern rule where every close is
// standard, the contract code enforces, and the ref-kind vocabulary, which
// one line names. Pre-flight's own prompt keeps each.
func TestPreflightRubricLeavesOutWhatCodeDecides(t *testing.T) {
	facts, err := basefacts.Entries(viewlayout.Vocabulary{})
	if err != nil {
		t.Fatal(err)
	}
	gap := &model.Entry{ID: "20260101-000002-s-tac-gap", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Content: "The gap body."}
	fact := &model.Entry{ID: "20260101-000001-s-cpt-fct", Type: model.TypeSignal, Kind: model.KindFact, Layer: model.LayerConceptual, Content: "The fact body."}
	question := &model.Entry{ID: "20260101-000003-s-cpt-qst", Type: model.TypeSignal, Kind: model.KindQuestion, Layer: model.LayerConceptual, Content: "The question body."}
	plan := &model.Entry{ID: "20260101-000004-d-tac-pln", Type: model.TypeDecision, Kind: model.KindPlan, Layer: model.LayerTactical, Content: "The plan body.\n\n## Acceptance criteria\n\n- [ ] one"}
	typeSystem := &model.Entry{ID: model.TypeSystemContractID, Type: model.TypeDecision, Kind: model.KindContract, Layer: model.LayerConceptual, Content: "The type-system contract body."}
	contract := &model.Entry{ID: "20260101-000005-d-cpt-con", Type: model.TypeDecision, Kind: model.KindContract, Layer: model.LayerConceptual, Content: "The other contract body."}
	graph := model.NewGraph(append(slices.Clone(facts), gap, fact, question, plan, typeSystem, contract))

	render := func(t *testing.T, g *model.Graph, draft *model.Entry) (prompt, rubric string) {
		t.Helper()
		var req llm.Request
		runner := llm.RunnerFunc(func(_ context.Context, r llm.Request) (llm.Result, error) {
			req = r
			return llm.Result{Text: `{"findings":[]}`}, nil
		})
		_, err := llmops.Preflight(t.Context(), runner, draft, g, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		rubric, err = llmops.PreflightRubric(draft, g, "")
		if err != nil {
			t.Fatal(err)
		}
		return req.SystemPrompt + "\n\n" + req.UserPrompt, rubric
	}
	refs := []model.Ref{{ID: gap.ID, Kind: model.RefKindGroundedIn}}

	t.Run("a plan's acceptance criteria", func(t *testing.T) {
		prompt, rubric := render(t, graph, &model.Entry{Type: model.TypeDecision, Kind: model.KindPlan, Layer: model.LayerTactical, Content: "Draft plan.", Refs: refs})
		for _, rule := range []string{"**Acceptance criteria presence**", "- `high`: proposed entry is a plan decision with no `## Acceptance criteria` section"} {
			if strings.Contains(rubric, rule) || !strings.Contains(prompt, rule) {
				t.Errorf("the rule the draft validation decides should leave the rules text only: %q", rule)
			}
		}
	})

	t.Run("ref applicability and the vocabulary", func(t *testing.T) {
		const applicability = "**Applicability is not your question.**"
		prompt, rubric := render(t, graph, &model.Entry{Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Content: "Draft directive.", Refs: refs})
		if strings.Contains(rubric, applicability) || !strings.Contains(prompt, applicability) {
			t.Errorf("the applicability paragraph should leave the rules text only")
		}
		_, body, err := graph.FactBody(basefacts.RefKindsFactID)
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(body, "\n") {
			if strings.HasPrefix(line, "| ") && strings.Contains(rubric, line) {
				t.Errorf("the rules text should hold no row of the ref-kind table: %q", line)
			}
		}
		var pointers []string
		for line := range strings.SplitSeq(rubric, "\n") {
			if strings.Contains(line, basefacts.RefKindsFactID) {
				pointers = append(pointers, line)
			}
		}
		if len(pointers) != 1 || !strings.Contains(pointers[0], "open the ref-kind fact") || !strings.Contains(pointers[0], "kind or desc fits the body") {
			t.Errorf("one line should name the ref-kind fact and say when to open it, got %q", pointers)
		}
		fit, err := llmops.DeepRefFit([]model.RefFit{model.FitRef(graph, refs[0])})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(fit, "Admissible ref kinds for this target") || !strings.Contains(fit, "Applicability is settled mechanically. Do not flag it.") {
			t.Errorf("each Ref fit line should still name the admissible kinds and settle applicability, got:\n%s", fit)
		}
	})

	t.Run("close patterns", func(t *testing.T) {
		const heading = "## Unusual close-pattern check"
		section := func(text string) string {
			_, rest, found := strings.Cut(text, heading)
			if !found {
				return ""
			}
			rule, _, _ := strings.Cut(rest, "\n## ")
			return rule
		}
		for _, tc := range []struct {
			name     string
			draft    *model.Entry
			standard bool
		}{
			{"plan closes a gap", &model.Entry{Type: model.TypeDecision, Kind: model.KindPlan, Layer: model.LayerTactical, Content: "Draft.", Closes: []string{gap.ID}}, true},
			{"done closes a plan and a gap", &model.Entry{Type: model.TypeSignal, Kind: model.KindDone, Layer: model.LayerTactical, Content: "Draft.", Closes: []string{plan.ID, gap.ID}}, true},
			{"fact closes a question", &model.Entry{Type: model.TypeSignal, Kind: model.KindFact, Layer: model.LayerConceptual, Content: "Draft.", Closes: []string{question.ID}}, true},
			{"fact closes a fact", &model.Entry{Type: model.TypeSignal, Kind: model.KindFact, Layer: model.LayerConceptual, Content: "Draft.", Closes: []string{fact.ID}}, false},
			{"done closes a fact beside a plan", &model.Entry{Type: model.TypeSignal, Kind: model.KindDone, Layer: model.LayerTactical, Content: "Draft.", Closes: []string{plan.ID, fact.ID}}, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				prompt, rubric := render(t, graph, tc.draft)
				want := section(prompt)
				if want == "" {
					t.Fatal("pre-flight's prompt should keep the close-pattern rule")
				}
				got := section(rubric)
				if tc.standard && got != "" {
					t.Errorf("a draft whose closes are all standard should get no close-pattern rule")
				}
				if !tc.standard && got != want {
					t.Errorf("a draft with an unusual close should get the close-pattern rule in full, got:\n%s", got)
				}
			})
		}
	})

	t.Run("contracts code enforces", func(t *testing.T) {
		draft := &model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Content: "Draft gap."}
		prompt, rubric := render(t, graph, draft)
		if strings.Contains(rubric, typeSystem.Content) || !strings.Contains(prompt, typeSystem.Content) {
			t.Errorf("the contract code enforces should leave the rules text only")
		}
		if !strings.Contains(rubric, contract.Content) {
			t.Errorf("a contract code does not name should stay in the rules text")
		}
		successor := &model.Entry{ID: "20260102-000000-d-cpt-suc", Type: model.TypeDecision, Kind: model.KindContract, Layer: model.LayerConceptual, Content: "The successor contract body.", Supersedes: []string{typeSystem.ID}}
		_, rubric = render(t, model.NewGraph(append(slices.Clone(facts), typeSystem, successor)), draft)
		if !strings.Contains(rubric, successor.Content) {
			t.Errorf("a successor of the contract code enforces should stay in the rules text until code names it")
		}
	})
}
