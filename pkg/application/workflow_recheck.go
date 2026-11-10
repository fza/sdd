package application

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/serveview"
)

// registerWorkflowRecheck registers the catch-up's recheck section
// (20261109-004506-d-cpt-udz, D5).
func (w *WorkflowSession) registerWorkflowRecheck(registry *engine.Registry) error {
	return registry.RegisterQuery(engine.Query{
		Doc: engine.FuncDoc{
			Name: "recheckItems",
			Doc: "The recheck list: strains between existing entries and hand edits of committed entries no later entry healed, " +
				"with the healing suggestion. Shown only to the participants recheck.responsible names, to everyone while it names none.",
		},
		ServeSafe: true,
		Bound:     engine.QueryBound{Part: serveview.PartText, Cap: serveview.Default().Cap(serveview.PartText)},
		Fn: func(ctx *engine.Context, _ map[string]any) (any, error) {
			runtime, err := w.targetRuntime(w.instanceProject(ctx.Instance), AccessRead)
			if err != nil {
				return nil, err
			}
			info, err := w.readInfo()
			if err != nil {
				return nil, err
			}
			if !recheckResponsible(runtime.options.Grounding, info.Participant) {
				return "", nil
			}
			open, err := runtime.openRecheck(w.ctx, ctx.Graph)
			if err != nil {
				return nil, err
			}
			var rendered bytes.Buffer
			presenters.RenderRecheckItems(&rendered, ctx.Graph, open)
			return strings.TrimRight(rendered.String(), "\n"), nil
		},
	})
}

func storeStrains(store *engine.Store) []model.Strain {
	var strains []model.Strain
	for _, doc := range workflowStoreDocuments(store, "strains") {
		relation, _ := doc["relation"].(string)
		ids, _ := doc["ids"].([]any)
		if len(ids) != 2 {
			continue
		}
		a, _ := ids[0].(string)
		b, _ := ids[1].(string)
		strains = append(strains, model.Strain{IDs: [2]string{a, b}, Relation: relation})
	}
	return strains
}

// storeCaptureRecheck records the capture's strains as recheck items with
// their relation bits in the ledger, and scans for hand edits since the
// baseline (5.9).
func (w *WorkflowSession) storeCaptureRecheck(ctx *engine.Context, project ProjectID, entryID string) error {
	runtime, err := w.targetRuntime(project, AccessWrite)
	if err != nil {
		return err
	}
	now := w.app.clock.Now()
	var items []model.RecheckItem
	var batch model.RelevanceBatch
	for _, strain := range storeStrains(ctx.Store) {
		bit, err := model.StrainRelationBit(strain.Relation)
		if err != nil {
			return err
		}
		items = append(items, model.StrainItem(strain, entryID, now))
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: model.NewRelevancePair(strain.IDs[0], strain.IDs[1]), Verdict: model.ReaderVerdict{Relations: bit}})
	}
	if len(items) > 0 && runtime.options.Events == nil {
		return fmt.Errorf("the capture reports strains, but project %s has no event store to keep them", runtime.options.Project.ID)
	}
	err = runtime.appendRelevance(w.ctx, batch)
	if err != nil {
		return err
	}
	err = runtime.appendRecheck(w.ctx, items)
	if err != nil {
		return err
	}
	_, err = runtime.scanHandEdits(w.ctx, now)
	return err
}

// strainRefusals names the unhealed strains the draft builds on (D26), each
// with the ways out.
func (w *WorkflowSession) strainRefusals(ctx *engine.Context, runtime *ProjectRuntime, cited []string) ([]string, error) {
	open, err := runtime.openRecheck(w.ctx, ctx.Graph)
	if err != nil {
		return nil, err
	}
	var refusals []string
	for _, item := range model.StrainRefusals(open, cited) {
		refusals = append(refusals, fmt.Sprintf("the draft cites %s while %s of %s stays unhealed (recheck item %s): heal it by citing both, redraft around the clash, hold the draft as WIP, or drop the topic. Ask the user where the choice is theirs",
			strings.Join(citedOf(item.Entries, cited), ", "), item.Relation, strings.Join(item.Entries, " and "), item.ID))
	}
	return refusals, nil
}

func citedOf(entries, cited []string) []string {
	var out []string
	for _, id := range entries {
		if slices.Contains(cited, id) {
			out = append(out, id)
		}
	}
	return out
}
