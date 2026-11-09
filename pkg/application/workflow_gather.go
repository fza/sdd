package application

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/engine"
)

// WorkflowGatherRequest asks for a grounding serve inside the session.
// Instance names the running capture the reading credits; its draft (body,
// kind, topics) writes the queries. Without an instance Draft and Topics
// describe the draft and nothing is credited.
type WorkflowGatherRequest struct {
	Instance string
	IDs      []string
	Draft    string
	Topics   []string
	SyncMode SearchSyncMode
	// Held reports whether the consumer already holds an entry in full.
	Held func(id string) bool
}

// Gather runs the reading and, naming a running instance, records it as the
// instance's credited read (D28, D31): only reads made through it inside the
// instance count for the capture's grounding.
func (w *WorkflowSession) Gather(ctx context.Context, identity RequestIdentity, request WorkflowGatherRequest) (GatherResult, error) {
	w.setOperation(ctx, identity)
	gather := GatherRequest{SyncMode: request.SyncMode, Draft: request.Draft, Topics: request.Topics, IDs: request.IDs, Held: request.Held}
	project := w.project
	gather.Branch, gather.BranchFromSession = w.branch, w.branch != ""
	if request.Instance != "" {
		inst, ok := w.session.Instance(request.Instance)
		if !ok {
			return GatherResult{}, fmt.Errorf("instance %q not found in session", request.Instance)
		}
		if inst.Status != engine.StatusRunning {
			return GatherResult{}, fmt.Errorf("instance %s has ended (%s): gather credits only a running capture", request.Instance, inst.Outcome)
		}
		project = w.instanceProject(inst.ID)
		target, fromBinding := w.effectiveTarget(inst.Store)
		gather.Branch, gather.BranchFromSession = target.Branch, fromBinding
		draft := w.draftFromStore(inst.Store)
		if gather.Draft == "" {
			gather.Draft = draft.Body
		}
		if len(gather.Topics) == 0 {
			gather.Topics = draft.Topics
		}
		gather.Kind = draft.Kind
		gather.Answered = append(captureAnswered(inst.Store), storeVerdictIDs(inst.Store)...)
		gather.since = inst.Reading().Corpus
	}
	result, err := w.app.Gather(ctx, identity, project, gather)
	if err != nil {
		return GatherResult{}, err
	}
	if request.Instance == "" {
		w.session.LogRead("gather", result.FullIDs, result.SummaryIDs)
		return result, w.session.SinkErr()
	}
	err = w.session.Record(engine.Read{Tool: "gather", Instance: request.Instance, Full: result.FullIDs, Summary: result.SummaryIDs, Grounding: result.payload})
	if err != nil {
		return GatherResult{}, err
	}
	return result, nil
}

// captureAnswered returns the entries a capture's draft already answers:
// every ref, supersede and close target.
func captureAnswered(store *engine.Store) []string {
	var answered []string
	for _, ref := range workflowStoreDocuments(store, "refs") {
		if id, _ := ref["id"].(string); id != "" {
			answered = append(answered, id)
		}
	}
	answered = append(answered, workflowStoreStrings(store, "supersedes")...)
	return append(answered, workflowStoreStrings(store, "closes")...)
}
