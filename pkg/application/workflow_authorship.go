package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/serveview"
)

const fieldReason = "reason"

// agentAuthor is the MCP client whose agent an autonomous capture writes for.
type agentAuthor struct {
	client string
}

const agentParticipantsRule = "participants are not reported in auto mode: sdd writes the actor of your MCP client"

// approvalAuthor carries the person's own words for an approved draft's reason
// and a directive's intent, each empty when they gave none.
type approvalAuthor struct {
	reasonWords, intentWords string
}

// markDraft marks whose a capture's draft is (model.Entry.Approved): the
// person's once they approved it at an assisted session's gate, and so in the
// checks an assisted session runs before that gate; otherwise the agent's
// where the agent answers.
func (w *WorkflowSession) markDraft(ctx *engine.Context, draft *EntryDraft, beforeGate bool) error {
	if engine.Approved(ctx.Store) || beforeGate && w.assisted() {
		reason, _ := workflowStoreString(ctx.Store, fieldReasonWords)
		intent, _ := workflowStoreString(ctx.Store, fieldIntentWords)
		draft.approval = &approvalAuthor{reasonWords: reason, intentWords: intent}
		return nil
	}
	return w.markAgentDraft(ctx.Instance, draft)
}

// markAgentDraft marks the draft of an autonomous capture as the agent's, of
// the client that started instance.
func (w *WorkflowSession) markAgentDraft(instance string, draft *EntryDraft) error {
	if !w.Autonomous() {
		return nil
	}
	client, err := w.startingClient(instance)
	if err != nil {
		return err
	}
	if strings.TrimSpace(client) == "" {
		return errNamelessClient
	}
	draft.agent = &agentAuthor{client: client}
	return nil
}

var errNamelessClient = errors.New("an unattended capture writes for the MCP client that started it, and this client sent no name: send a name in the client info to capture unattended")

// refuseNamelessCapture refuses starting an autonomous move that writes an
// entry from a client that sent no name.
func (w *WorkflowSession) refuseNamelessCapture(spec *engine.Spec, client string) error {
	if w.Autonomous() && !w.assisted() && publishesEntry(spec) && strings.TrimSpace(client) == "" {
		return errNamelessClient
	}
	return nil
}

type instanceClientEvent struct {
	Instance string `json:"instance"`
	Client   string `json:"client"`
}

func instanceClientRecord(instance, client string) (StoredEvent, error) {
	payload, err := json.Marshal(instanceClientEvent{Instance: instance, Client: client})
	if err != nil {
		return StoredEvent{}, err
	}
	return StoredEvent{CodecVersion: SessionCodecVersion, Code: workflowInstanceClientCode, Payload: payload}, nil
}

func (w *WorkflowSession) restoreInstanceClients(events []StoredEvent) error {
	for _, stored := range events {
		if stored.Code != workflowInstanceClientCode {
			continue
		}
		if !SupportedSessionCodecVersion(stored.CodecVersion) {
			return &ApplicationError{Code: ErrorMigrationRequired, Message: "unsupported instance client event codec", Version: stored.CodecVersion}
		}
		var event instanceClientEvent
		if err := json.Unmarshal(stored.Payload, &event); err != nil {
			return fmt.Errorf("decoding instance client event: %w", err)
		}
		if event.Instance == "" {
			return fmt.Errorf("decoding instance client event: instance is required")
		}
		w.instanceClients[event.Instance] = event.Client
	}
	return nil
}

// startingClient is the MCP client that started instance, else the one that
// started its nearest started ancestor; a later attach never changes it. A
// session logged before these records falls back to its last attachment.
func (w *WorkflowSession) startingClient(instance string) (string, error) {
	for id := instance; id != ""; {
		if client, ok := w.instanceClients[id]; ok {
			return client, nil
		}
		inst, ok := w.session.Instance(id)
		if !ok {
			break
		}
		id = inst.Parent
	}
	return w.attachedClient(w.ctx)
}

// authorshipMissing lists what an autonomous draft's authorship fields owe: a
// decision's own reason, no reason on a signal, no participants. An assisted
// draft owes the reason too, written when the person approves without one of
// theirs, and may name participants, as the entry is the person's. A guided
// draft owes none; its reason is the user's, in reasonWords, and sdd ignores
// the reason field there.
func (w *WorkflowSession) authorshipMissing(store *engine.Store) []string {
	if !w.Autonomous() {
		return nil
	}
	kind, _ := workflowStoreString(store, "entryKind")
	reason, _ := workflowStoreString(store, fieldReason)
	decision := kind != "" && model.IsValidKindForType(model.TypeDecision, model.Kind(kind))
	var missing []string
	switch {
	case decision && strings.TrimSpace(reason) == "":
		missing = append(missing, fmt.Sprintf("a decision in %s mode states your own reason for it in reason", w.session.DialogueMode))
	case !decision && reason != "":
		missing = append(missing, "a signal carries no reason")
	}
	if !w.assisted() && len(workflowStoreStrings(store, "participants")) > 0 {
		missing = append(missing, agentParticipantsRule)
	}
	return missing
}

// agentLimitRule is the rule that parks the draft for a person in auto mode,
// or "" when the agent may write it alone; an assisted draft is the person's
// to approve, so none binds it.
func (w *WorkflowSession) agentLimitRule(ctx *engine.Context) string {
	if !w.Autonomous() || w.assisted() || ctx.Graph == nil {
		return ""
	}
	entry, findings := entryFromDraft(w.draftFromStore(ctx.Store), "", w.app.now())
	if len(findings) > 0 {
		return ""
	}
	for _, ids := range []*[]string{&entry.Supersedes, &entry.Closes, &entry.Approves} {
		if resolved, err := ctx.Graph.ResolveIDs(*ids); err == nil {
			*ids = resolved
		}
	}
	return ctx.Graph.AgentLimit(entry)
}

func (w *WorkflowSession) registerAuthorship(registry *engine.Registry) error {
	err := registry.RegisterQuery(engine.Query{
		Doc: engine.FuncDoc{
			Name: "agentChains",
			Doc:  "The chains of agent reasoning awaiting a person's review (model.Graph.AgentChains): each live root an agent decided with no approval, and the marked entries resting on it.",
		},
		ServeSafe: true,
		Bound:     engine.QueryBound{Part: serveview.PartText, Cap: serveview.Default().Cap(serveview.PartText)},
		Fn: func(ctx *engine.Context, _ map[string]any) (any, error) {
			var rendered bytes.Buffer
			presenters.RenderAgentChains(&rendered, ctx.Graph.AgentChains())
			return strings.TrimRight(rendered.String(), "\n"), nil
		},
	})
	if err != nil {
		return err
	}
	err = registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{
			Name:  "authorshipHeld",
			Doc:   "In auto mode a decision states the agent's own reason, a signal none, and no draft reports participants; a guided draft owes nothing here.",
			Reads: []string{"entryKind", fieldReason, "participants"},
		},
		Fn: func(ctx *engine.Context) (bool, error) {
			return len(w.authorshipMissing(ctx.Store)) == 0, nil
		},
		FailMessage: "the draft's authorship fields do not fit the dialogue mode",
		FailDetail: func(ctx *engine.Context) string {
			return strings.Join(w.authorshipMissing(ctx.Store), "; ")
		},
	})
	if err != nil {
		return err
	}
	return registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{
			Name: "agentMayWrite",
			Doc: "The session is guided, or the agent may write the draft alone (model.Graph.AgentLimit): every signal kind, " +
				"and plans, activities and pending directives on the tactical and operational layers, no approval, and no supersede or close of a person's entry but a done's close.",
			Reads: []string{"entryKind", "layer", "intent", "supersedes", "closes", "approves"},
		},
		Fn: func(ctx *engine.Context) (bool, error) {
			return w.agentLimitRule(ctx) == "", nil
		},
		FailMessage: "an agent alone may not write this draft",
		FailDetail:  w.agentLimitRule,
		Selector:    true,
	})
}
