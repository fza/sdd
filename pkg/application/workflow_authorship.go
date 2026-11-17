package application

import (
	"bytes"
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

// markAgentDraft marks the draft of an autonomous capture as the agent's, of
// the client the session last attached through.
func (w *WorkflowSession) markAgentDraft(draft *EntryDraft) error {
	if !w.Autonomous() {
		return nil
	}
	client, err := w.attachedClient(w.ctx)
	if err != nil {
		return err
	}
	draft.agent = &agentAuthor{client: client}
	return nil
}

// authorshipMissing lists what an autonomous draft's authorship fields owe: a
// decision's own reason, no reason on a signal, no participants. A guided
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
		missing = append(missing, "a decision in auto mode states your own reason for it in reason")
	case !decision && reason != "":
		missing = append(missing, "a signal carries no reason")
	}
	if len(workflowStoreStrings(store, "participants")) > 0 {
		missing = append(missing, agentParticipantsRule)
	}
	return missing
}

// agentLimitRule is the rule that parks the draft for a person in auto mode,
// or "" when the agent may write it alone.
func (w *WorkflowSession) agentLimitRule(ctx *engine.Context) string {
	if !w.Autonomous() || ctx.Graph == nil {
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
