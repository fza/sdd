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

const reasonUnstoredLine = "No participant is configured, so the reason is not stored on the entry: set one with `sdd config set --local participant <name>`."

const agentParticipantsRule = "participants are not reported in auto mode: sdd writes the actor of your MCP client"

// approvalAuthor marks a draft the person approves: given once they did.
type approvalAuthor struct {
	given bool
}

// markDraft marks whose a capture's draft is (model.Entry.Approved): the
// person's once they approved it at an assisted session's gate, and so in the
// checks an assisted session runs before that gate; otherwise the agent's
// where the agent answers.
func (w *WorkflowSession) markDraft(ctx *engine.Context, draft *EntryDraft, beforeGate bool) error {
	if approved := engine.Approved(ctx.Store); approved || beforeGate && w.assisted() {
		draft.approval = &approvalAuthor{given: approved}
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

// unknownClientRule names the fix for an autonomous capture from a client no
// actor names: sdd never writes the client's raw name as participant.
func unknownClientRule(client string) string {
	return fmt.Sprintf("no actor names the MCP client %q, and in auto mode sdd writes the client's actor as the participant: "+
		"capture an actor signal for the MCP client %q first (kind actor, a canonical such as Claude, %q among its aliases), then report this draft again", client, client, client)
}

// clientActorRule is unknownClientRule for an autonomous draft whose
// starting client resolves to no actor (model.Graph.ClientParticipant), else
// "".
func (w *WorkflowSession) clientActorRule(ctx *engine.Context) (string, error) {
	if !w.Autonomous() || w.assisted() {
		return "", nil
	}
	if ctx.Graph == nil {
		return "", errors.New("clientActorKnown needs a graph")
	}
	client, err := w.startingClient(ctx.Instance)
	if err != nil || strings.TrimSpace(client) == "" {
		return "", err
	}
	kind, _ := workflowStoreString(ctx.Store, "entryKind")
	canonical, _ := workflowStoreString(ctx.Store, "canonical")
	draft := &model.Entry{Kind: model.Kind(kind), Canonical: canonical, Aliases: workflowStoreStrings(ctx.Store, "aliases")}
	if ctx.Graph.ClientParticipant(draft, client) != "" {
		return "", nil
	}
	return unknownClientRule(client), nil
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

// authorshipMissing lists what an autonomous draft's authorship fields break:
// a reason on a signal, participants. An assisted draft may name participants,
// as the entry is the person's. A guided draft breaks none: the person's
// confirm makes its reason theirs.
func (w *WorkflowSession) authorshipMissing(store *engine.Store) []string {
	if !w.Autonomous() {
		return nil
	}
	kind, _ := workflowStoreString(store, "entryKind")
	reason, _ := workflowStoreString(store, fieldReason)
	decision := kind != "" && model.IsValidKindForType(model.TypeDecision, model.Kind(kind))
	var missing []string
	if !decision && reason != "" {
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
			Doc:   "In auto and assisted mode a signal carries no reason, and an auto draft reports no participants; a guided draft owes nothing here.",
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
	err = registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{
			Name:  "confirmFitsDialogue",
			Doc:   "The agent answers the person's choices, or the playback confirmation is not the agent's: a guided write is the person's and rests on their own confirm.",
			Reads: []string{"playbackConfirmation"},
		},
		Fn: func(ctx *engine.Context) (bool, error) {
			return w.Autonomous() || !engine.ConfirmedByAgent(ctx.Store), nil
		},
		FailMessage: "the agent confirmed this draft, and the person has not",
	})
	if err != nil {
		return err
	}
	err = registry.RegisterQuery(engine.Query{
		Doc: engine.FuncDoc{
			Name:  "reasonUnstored",
			Doc:   "In a guided capture of a decision with a reason, the line naming that no participant is configured, so the reason is not stored (markConfirmed); empty otherwise.",
			Reads: []string{"entryKind", fieldReason, fieldReasonWords},
		},
		ServeSafe: true,
		Bound:     engine.QueryBound{Part: serveview.PartText, Cap: serveview.Cap{MaxBytes: len(reasonUnstoredLine)}},
		Fn: func(ctx *engine.Context, _ map[string]any) (any, error) {
			if w.Autonomous() || strings.TrimSpace(w.draftFromStore(ctx.Store).Reason) == "" {
				return "", nil
			}
			kind, _ := workflowStoreString(ctx.Store, "entryKind")
			if !model.IsValidKindForType(model.TypeDecision, model.Kind(kind)) {
				return "", nil
			}
			info, err := w.readInfo()
			if err != nil || strings.TrimSpace(info.Participant) != "" {
				return "", err
			}
			return reasonUnstoredLine, nil
		},
	})
	if err != nil {
		return err
	}
	err = registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{
			Name:  "clientActorKnown",
			Doc:   "In auto mode an actor names the MCP client that started the capture, or the draft is that actor: sdd writes the actor as the participant, never the client's name.",
			Reads: []string{"entryKind", "canonical", "aliases"},
		},
		Fn: func(ctx *engine.Context) (bool, error) {
			rule, err := w.clientActorRule(ctx)
			return rule == "", err
		},
		FailMessage: "no actor names your MCP client",
		FailDetail: func(ctx *engine.Context) string {
			rule, _ := w.clientActorRule(ctx)
			return rule
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
