package llmops_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/viewlayout"
	"github.com/networkteam/sdd/pkg/llm"
)

// rubricShapes are the draft shapes the composition of the rules text
// measures (attachment of 20261122-101625-d-tac-r0s), over a graph holding
// one entry of each kind they reference, close or supersede.
func rubricShapes(t *testing.T) (*model.Graph, []rubricShape) {
	t.Helper()
	facts, err := basefacts.Entries(viewlayout.Vocabulary{})
	if err != nil {
		t.Fatal(err)
	}
	gap := &model.Entry{ID: "20260101-000001-s-tac-gap", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Content: "The gap body."}
	fact := &model.Entry{ID: "20260101-000002-s-cpt-fct", Type: model.TypeSignal, Kind: model.KindFact, Layer: model.LayerConceptual, Content: "The fact body."}
	question := &model.Entry{ID: "20260101-000003-s-cpt-qst", Type: model.TypeSignal, Kind: model.KindQuestion, Layer: model.LayerConceptual, Content: "The question body."}
	plan := &model.Entry{ID: "20260101-000004-d-tac-pln", Type: model.TypeDecision, Kind: model.KindPlan, Layer: model.LayerTactical, Content: "The plan body.\n\n## Acceptance criteria\n\n- [ ] one"}
	directive := &model.Entry{ID: "20260101-000005-d-tac-dir", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Content: "The directive body."}
	aspiration := &model.Entry{ID: "20260101-000006-d-stg-asp", Type: model.TypeDecision, Kind: model.KindAspiration, Layer: model.LayerStrategic, Content: "The aspiration body."}
	typeSystem := &model.Entry{ID: model.TypeSystemContractID, Type: model.TypeDecision, Kind: model.KindContract, Layer: model.LayerConceptual, Content: "The type-system contract body."}
	contract := &model.Entry{ID: "20260101-000007-d-cpt-con", Type: model.TypeDecision, Kind: model.KindContract, Layer: model.LayerConceptual, Content: "The other contract body."}
	graph := model.NewGraph(append(facts, gap, fact, question, plan, directive, aspiration, typeSystem, contract))

	ref := []model.Ref{{ID: gap.ID, Kind: model.RefKindGroundedIn}}
	signal := func(kind model.Kind) model.Entry {
		return model.Entry{Type: model.TypeSignal, Kind: kind, Layer: model.LayerTactical, Content: "Draft body."}
	}
	decision := func(kind model.Kind, layer model.Layer) model.Entry {
		return model.Entry{Type: model.TypeDecision, Kind: kind, Layer: layer, Content: "Draft body."}
	}
	shape := func(name string, e model.Entry, edit func(*model.Entry)) rubricShape {
		if edit != nil {
			edit(&e)
		}
		return rubricShape{name: name, draft: &e}
	}
	withRef := func(e *model.Entry) { e.Refs = ref }
	settled := func(e *model.Entry) { e.Intent = model.IntentSettled }
	closes := func(ids ...string) func(*model.Entry) { return func(e *model.Entry) { e.Closes = ids } }
	both := func(edits ...func(*model.Entry)) func(*model.Entry) {
		return func(e *model.Entry) {
			for _, edit := range edits {
				edit(e)
			}
		}
	}
	return graph, []rubricShape{
		shape("gap signal, no refs", signal(model.KindGap), nil),
		shape("gap signal, 1 ref", signal(model.KindGap), withRef),
		shape("plan, 1 ref", decision(model.KindPlan, model.LayerTactical), withRef),
		shape("tactical directive, 1 ref", decision(model.KindDirective, model.LayerTactical), withRef),
		shape("plan, 1 ref, closes a gap", decision(model.KindPlan, model.LayerTactical), both(withRef, closes(gap.ID))),
		shape("conceptual directive, 1 ref", decision(model.KindDirective, model.LayerConceptual), withRef),
		shape("settled tactical directive, 1 ref", decision(model.KindDirective, model.LayerTactical), both(withRef, settled)),
		shape("settled conceptual directive, 1 ref", decision(model.KindDirective, model.LayerConceptual), both(withRef, settled)),
		shape("tactical directive, no refs", decision(model.KindDirective, model.LayerTactical), nil),
		shape("tactical directive, closes a gap", decision(model.KindDirective, model.LayerTactical), closes(gap.ID)),
		shape("settled tactical directive, 1 ref, closes a fact", decision(model.KindDirective, model.LayerTactical), both(withRef, settled, closes(fact.ID))),
		shape("done, 1 ref, closes a plan", signal(model.KindDone), both(withRef, closes(plan.ID))),
		shape("done, closes a directive", signal(model.KindDone), closes(directive.ID)),
		shape("done, closes a gap", signal(model.KindDone), closes(gap.ID)),
		shape("fact, closes a question", signal(model.KindFact), closes(question.ID)),
		shape("gap, closes a fact", signal(model.KindGap), closes(fact.ID)),
		shape("fact, closes a fact", signal(model.KindFact), closes(fact.ID)),
		shape("gap, supersedes a gap", signal(model.KindGap), func(e *model.Entry) { e.Supersedes = []string{gap.ID} }),
		shape("aspiration", decision(model.KindAspiration, model.LayerStrategic), nil),
		shape("actor", signal(model.KindActor), nil),
		shape("role, 1 ref", decision(model.KindRole, model.LayerTactical), withRef),
		shape("annotation, 1 ref", signal(model.KindAnnotation), withRef),
		shape("focus, 1 ref", decision(model.KindFocus, model.LayerTactical), withRef),
	}
}

type rubricShape struct {
	name  string
	draft *model.Entry
}

// Pre-flight's own prompt and the rules text the deep verifier reads, per
// draft shape. Each line of the rules text opens a line of pre-flight's
// prompt, apart from the prose verifier's axes it ends with and the line
// naming the ref-kind fact; the snapshots record both texts whole, so their
// diff reviews any change to either.
func TestPreflightPromptAndRubricPerDraftShape(t *testing.T) {
	graph, shapes := rubricShapes(t)
	for _, tc := range shapes {
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

			promptLines := strings.Split(req.SystemPrompt+"\n"+req.UserPrompt, "\n")
			rules, _, found := strings.Cut(rubric, "## Graph fit against the grounding reading")
			if !found {
				t.Fatalf("the rules text should end with the prose verifier's axes")
			}
			for line := range strings.SplitSeq(rules, "\n") {
				if line == "" || strings.Contains(line, basefacts.RefKindsFactID) {
					continue
				}
				if !slices.ContainsFunc(promptLines, func(p string) bool { return strings.HasPrefix(p, line) }) {
					t.Errorf("the rules text should hold no line pre-flight's prompt lacks: %q", line)
				}
			}

			snapshot := cupaloy.New(cupaloy.SnapshotFileExtension(".md"))
			t.Run("prompt", func(t *testing.T) {
				snapshot.SnapshotT(t, "# System\n\n"+req.SystemPrompt+"\n\n# User\n\n"+req.UserPrompt)
			})
			t.Run("rules text", func(t *testing.T) {
				snapshot.SnapshotT(t, rubric)
			})
		})
	}
}
