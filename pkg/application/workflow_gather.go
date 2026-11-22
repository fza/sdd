package application

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/engine"
)

// WorkflowGatherRequest asks for a grounding serve inside the session.
// Instance names the running capture the reading credits; its draft (body,
// kind, topics) is searched with its own text and its topics listed.
// Without an instance Draft and Topics describe the draft and nothing is
// credited; with one, Topics are listed beside the draft's own. Query and
// Terms are the agent's searches.
type WorkflowGatherRequest struct {
	Instance string
	IDs      []string
	Draft    string
	Topics   []string
	Query    string
	Terms    []string
	// Wait has the judge score until the minimum holds (GatherRequest.Wait).
	Wait bool
	// Rejudge has the judge score every candidate again (GatherRequest.Rejudge).
	Rejudge  bool
	SyncMode SearchSyncMode
}

// Gather runs the reading and, naming a running instance, records it as the
// instance's credited read: only reads made through it inside the instance
// count for the capture's grounding. Inside a reading already begun, a call
// naming only IDs opens those entries and lists the ones landed since that no
// credited read listed, and runs nothing else.
func (w *WorkflowSession) Gather(ctx context.Context, identity RequestIdentity, request WorkflowGatherRequest) (GatherResult, error) {
	w.setOperation(ctx, identity)
	gather := GatherRequest{SyncMode: request.SyncMode, Draft: request.Draft, Topics: request.Topics, IDs: request.IDs, Query: strings.TrimSpace(request.Query), Terms: nonEmpty(request.Terms), Wait: request.Wait, Rejudge: request.Rejudge, unattended: w.session.DialogueMode.AgentAnswers()}
	if repeats := w.repeats(); repeats != nil {
		gather.Held, gather.Served = repeats.Held, repeats.Served
	}
	client, err := w.attachedClient(ctx)
	if err != nil {
		return GatherResult{}, err
	}
	gather.client = client
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
		project = w.inCapture(inst, &gather, w.draftFromStore(inst.Store))
		gather.alsoListed = request.Topics
		gather.openOnly = len(request.IDs) > 0 && request.Draft == "" && len(request.Topics) == 0 && gather.Query == "" && len(gather.Terms) == 0 && gather.since != nil
	}
	w.demandProject(project)
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
	if err := w.rankInBackground(request.Instance); err != nil {
		return GatherResult{}, err
	}
	return result, nil
}

// inCapture points gather at the capture inst with draft and returns the
// project it reads.
func (w *WorkflowSession) inCapture(inst *engine.Instance, gather *GatherRequest, draft EntryDraft) ProjectID {
	target, fromBinding := w.effectiveTarget(inst.Store)
	gather.Branch, gather.BranchFromSession = target.Branch, fromBinding
	if gather.Draft == "" {
		gather.Draft = draft.Body
	}
	gather.Topics, gather.Kind = draft.Topics, draft.Kind
	gather.References = captureAnswered(inst.Store)
	gather.Answered = append(slices.Clone(gather.References), storeVerdictIDs(inst.Store)...)
	gather.since = inst.Reading().Corpus
	gather.reading = inst.Reading()
	return w.instanceProject(inst.ID)
}

// listLanded lists, as a report reaches a capture at assemble whose reading
// began, the entries landed since that no credited read listed, as a graph
// check lists them with the judge's marks, and credits the listing
// (20261121-223938-d-tac-rqi): no report is refused for them, and a landed
// line marked likely bearing owes its answer like any other. The draft is
// the stored one with the report's fields over it. Empty where nothing
// landed or the gate does not apply.
func (w *WorkflowSession) listLanded(ctx context.Context, identity RequestIdentity, instance string, report map[string]any) (string, error) {
	inst, ok := w.session.Instance(instance)
	if !ok || inst.Status != engine.StatusRunning || inst.Step != "assemble" || inst.Reading().Corpus == nil {
		return "", nil
	}
	graph, err := w.graphs.currentForInstance(inst.ID, inst.Store)
	if err != nil {
		return "", err
	}
	check := &engine.Context{Instance: inst.ID, Store: inst.Store, Graph: graph, Reading: inst.Reading()}
	evidence, applies, err := w.groundingEvidence(check, contextGraph(check))
	if err != nil || !applies || len(evidence.Unlisted()) == 0 {
		return "", err
	}
	draft := w.draftFromStore(inst.Store)
	if body, ok := report["body"].(string); ok && body != "" {
		draft.Body = body
	}
	if kind, ok := report["entryKind"].(string); ok && kind != "" {
		draft.Kind = kind
	}
	if topics, ok := report["topics"].([]any); ok {
		draft.Topics = nil
		for _, topic := range topics {
			if label, ok := topic.(string); ok {
				draft.Topics = append(draft.Topics, label)
			}
		}
	}
	gather := GatherRequest{SyncMode: SearchSyncLocal, openOnly: true, judgeLanded: true, unattended: w.session.DialogueMode.AgentAnswers()}
	if repeats := w.repeats(); repeats != nil {
		gather.Held, gather.Served = repeats.Held, repeats.Served
	}
	gather.client, err = w.attachedClient(ctx)
	if err != nil {
		return "", err
	}
	project := w.inCapture(inst, &gather, draft)
	w.demandProject(project)
	result, err := w.app.Gather(ctx, identity, project, gather)
	if err != nil {
		return "", fmt.Errorf("listing the entries landed since the reading began: %w", err)
	}
	err = w.session.Record(engine.Read{Tool: "inject:landedEntries", Instance: instance, Summary: result.SummaryIDs, Grounding: result.payload})
	if err != nil {
		return "", err
	}
	return "## Entries landed since the reading began, listed as read\n\n" + result.Text, nil
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
	answered = append(answered, workflowStoreStrings(store, "approves")...)
	return append(answered, workflowStoreStrings(store, "closes")...)
}

// nonEmpty returns the trimmed values that hold text.
func nonEmpty(values []string) []string {
	var out []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
