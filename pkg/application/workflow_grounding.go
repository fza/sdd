package application

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/serveview"
)

// groundingVerdictFields are the capture state fields carrying verdicts: the
// assemble report's and the late-entries answer's.
var groundingVerdictFields = []string{"verdicts", "lateVerdicts"}

// registerWorkflowGrounding registers the capture's grounding gate under
// grounding enforcement (20261109-004506-d-cpt-udz): the assemble predicate, the
// write step's late-entries check and the late-entries listing.
func (w *WorkflowSession) registerWorkflowGrounding(registry *engine.Registry) error {
	reads := []string{"entryKind", "topics", "refs", "supersedes", "closes", "verdicts", "lateVerdicts", "strains", "skipGrounding"}
	if err := registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{
			Name: "groundingRead",
			Doc: "Where verify.mode enforces grounding, the capture's credited reading is complete: the reads it owes ran through gather inside the capture, " +
				"every entry that landed since the reading began is listed, every entry served in full ends in refs or verdicts, every verdict and strain names a listed entry, and the draft cites no single entry of an unhealed strain. " +
				"Passes without enforcement, with skipGrounding, and for actor and role drafts.",
			Reads: reads,
		},
		Fn: func(ctx *engine.Context) (bool, error) {
			refusals, err := w.groundingRefusals(ctx)
			return len(refusals) == 0, err
		},
		FailMessage: "the capture's grounding reading is incomplete",
		FailDetail: func(ctx *engine.Context) string {
			refusals, err := w.groundingRefusals(ctx)
			if err != nil {
				return ""
			}
			return strings.Join(refusals, "; ")
		},
	}); err != nil {
		return err
	}
	if err := registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{
			Name:  "noLateEntries",
			Doc:   "Where verify.mode enforces grounding, no entry landed since the reading began that the capture has not listed or answered; passes where groundingRead passes vacuously.",
			Reads: reads,
		},
		Fn: func(ctx *engine.Context) (bool, error) {
			late, err := w.lateEntries(ctx, contextGraph(ctx))
			return len(late) == 0, err
		},
		FailMessage: "entries landed after the assemble check",
	}); err != nil {
		return err
	}
	return registry.RegisterQuery(engine.Query{
		// Not serve-safe: it records the listing as a credited read.
		Doc: engine.FuncDoc{
			Name:  "lateEntries",
			Doc:   "The entries that landed after the assemble check, one brief line each, recorded as the capture's late read.",
			Reads: reads,
		},
		Bound: engine.QueryBound{Part: serveview.PartEntryList},
		Fn: func(ctx *engine.Context, _ map[string]any) (any, error) {
			late, err := w.lateEntries(ctx, contextGraph(ctx))
			if err != nil || len(late) == 0 {
				return "", err
			}
			entries := make([]*model.Entry, 0, len(late))
			for _, id := range late {
				entries = append(entries, ctx.Graph.ByID[id])
			}
			err = w.session.Record(engine.Read{
				Tool: "inject:lateEntries", Instance: ctx.Instance, Summary: late,
				Grounding: model.ReadPayload{Reads: []model.GroundingRead{{Mode: model.GroundingLate, IDs: late}}},
			})
			if err != nil {
				return nil, err
			}
			var rendered bytes.Buffer
			presenters.RenderLateEntries(&rendered, ctx.Graph, entries, w.repeats())
			return strings.TrimRight(rendered.String(), "\n"), nil
		},
	})
}

// groundingEvidence returns the capture's grounding evidence over the graph
// loadGraph returns, loaded only once the gate applies, and
// whether the gate applies: grounding enforced, no recorded bypass, a gated
// kind.
func (w *WorkflowSession) groundingEvidence(ctx *engine.Context, loadGraph func() (*model.Graph, error)) (model.GroundingEvidence, bool, error) {
	runtime, err := w.targetRuntime(w.instanceProject(ctx.Instance), AccessRead)
	if err != nil {
		return model.GroundingEvidence{}, false, err
	}
	kind, _ := workflowStoreString(ctx.Store, "entryKind")
	if !runtime.enforcesGrounding(kind) {
		return model.GroundingEvidence{}, false, nil
	}
	skip, _ := ctx.Store.Get("skipGrounding")
	skipped, _ := skip.(bool)
	if skipped {
		return model.GroundingEvidence{}, false, nil
	}
	evidence := model.GroundingEvidence{
		Reading: ctx.Reading, Kind: model.Kind(kind),
		Answered: captureAnswered(ctx.Store), Verdicts: storeVerdicts(ctx.Store), Own: w.ownEntries(ctx.Instance),
	}
	if evidence.Exempt() {
		return model.GroundingEvidence{}, false, nil
	}
	for _, label := range workflowStoreStrings(ctx.Store, "topics") {
		topic, err := model.ParseTopicPath(label)
		if err == nil {
			evidence.Topics = append(evidence.Topics, topic)
		}
	}
	graph, err := loadGraph()
	if err != nil {
		return model.GroundingEvidence{}, false, err
	}
	for _, e := range model.GroundingPopulation(graph) {
		evidence.Live = append(evidence.Live, e.ID)
	}
	return evidence, true, nil
}

// groundingRefusals names what holds the assemble gate, each with the one
// call that clears it.
func (w *WorkflowSession) groundingRefusals(ctx *engine.Context) ([]string, error) {
	evidence, applies, err := w.groundingEvidence(ctx, contextGraph(ctx))
	if err != nil || !applies {
		return nil, err
	}
	gather := fmt.Sprintf("call gather with instance %s", ctx.Instance)
	if evidence.Reading == nil || evidence.Reading.Corpus == nil {
		return []string{"the capture has no credited reading: " + gather + " (only reads through gather inside the capture count), then report again"}, nil
	}
	var refusals []string
	owed := evidence.OwedReads()
	if len(owed) > 0 {
		refusals = append(refusals, "the reading lacks "+strings.Join(owed, ", ")+": "+gather+", then report again")
	}
	unlisted := evidence.Unlisted()
	if len(unlisted) > 0 {
		refusals = append(refusals, "entries landed since the reading began: "+strings.Join(unlisted, ", ")+": "+gather+" to list them, then report again")
	}
	open := evidence.OpenFullReads()
	if len(open) > 0 {
		refusals = append(refusals, "entries served in full end neither in refs nor in verdicts: "+strings.Join(open, ", ")+
			": report each in refs or in verdicts [{ids, code}] (codes: "+strings.Join(model.GroundingVerdictCodes(), ", ")+")")
	}
	unlistedVerdicts := evidence.UnlistedVerdicts()
	if len(unlistedVerdicts) > 0 {
		refusals = append(refusals, "verdicts name entries the reading never listed: "+strings.Join(unlistedVerdicts, ", ")+
			": "+gather+" and ids ["+strings.Join(unlistedVerdicts, ", ")+"] to open them, then report again")
	}
	var unlistedStrains []string
	for _, strain := range storeStrains(ctx.Store) {
		for _, id := range strain.IDs {
			if !evidence.Reading.IsListed(id) && !slices.Contains(unlistedStrains, id) {
				unlistedStrains = append(unlistedStrains, id)
			}
		}
	}
	if len(unlistedStrains) > 0 {
		refusals = append(refusals, "strains name entries the reading never listed: "+strings.Join(unlistedStrains, ", ")+
			": "+gather+" and ids ["+strings.Join(unlistedStrains, ", ")+"] to open them, then report again")
	}
	runtime, err := w.targetRuntime(w.instanceProject(ctx.Instance), AccessRead)
	if err != nil {
		return nil, err
	}
	strained, err := w.strainRefusals(ctx, runtime, evidence.Answered)
	if err != nil {
		return nil, err
	}
	return append(refusals, strained...), nil
}

// lateEntries returns the entries graph holds that landed after the assemble
// check and that the capture has not answered; none where the gate does not
// apply.
func (w *WorkflowSession) lateEntries(ctx *engine.Context, loadGraph func() (*model.Graph, error)) ([]string, error) {
	evidence, applies, err := w.groundingEvidence(ctx, loadGraph)
	if err != nil || !applies {
		return nil, err
	}
	return evidence.Late(), nil
}

// ownEntries returns the entries the session's other instances wrote.
func (w *WorkflowSession) ownEntries(instance string) []string {
	var own []string
	for _, inst := range w.session.Instances() {
		if inst.ID == instance {
			continue
		}
		if id, ok := workflowStoreString(inst.Store, "entryId"); ok {
			own = append(own, id)
		}
	}
	return own
}

func storeVerdicts(store *engine.Store) []model.GroundingVerdict {
	var verdicts []model.GroundingVerdict
	for _, field := range groundingVerdictFields {
		for _, doc := range workflowStoreDocuments(store, field) {
			code, _ := doc["code"].(string)
			verdict := model.GroundingVerdict{Code: code}
			ids, _ := doc["ids"].([]any)
			for _, raw := range ids {
				if id, ok := raw.(string); ok {
					verdict.IDs = append(verdict.IDs, id)
				}
			}
			verdicts = append(verdicts, verdict)
		}
	}
	return verdicts
}

func storeVerdictIDs(store *engine.Store) []string {
	var ids []string
	for _, verdict := range storeVerdicts(store) {
		ids = append(ids, verdict.IDs...)
	}
	return ids
}

// contextGraph loads the graph the engine resolved for ctx.
func contextGraph(ctx *engine.Context) func() (*model.Graph, error) {
	return func() (*model.Graph, error) { return ctx.Graph, nil }
}

// storeCaptureRelevance writes the capture's reader records, keyed to the
// entry it published, into the project's relevance ledger.
func (w *WorkflowSession) storeCaptureRelevance(ctx *engine.Context, project ProjectID, entryID string) error {
	if ctx.Reading == nil || ctx.Reading.Corpus == nil {
		return nil
	}
	runtime, err := w.targetRuntime(project, AccessWrite)
	if err != nil {
		return err
	}
	batch, err := model.CaptureRelevance(entryID, ctx.Reading, captureAnswered(ctx.Store), storeVerdicts(ctx.Store))
	if err != nil {
		return err
	}
	if err := runtime.appendRelevance(w.ctx, batch); err != nil {
		return err
	}
	return applyDriftRule(w.ctx, runtime, w.app.clock.Now())
}

// applyDriftRule steps the current judge's set-aside threshold down when
// the latest audit labels show too many misses (D36).
func applyDriftRule(ctx context.Context, runtime *ProjectRuntime, now time.Time) error {
	judge := runtime.options.Prefilter.Judge
	if judge == "" {
		return nil
	}
	ledger, err := runtime.relevanceLedger(ctx)
	if err != nil {
		return err
	}
	step, due := model.DriftStepDue(ledger.Labels(judge), ledger.Steps(), judge, now)
	if !due {
		return nil
	}
	return runtime.appendRelevance(ctx, model.RelevanceBatch{Steps: []model.DriftStep{step}})
}
