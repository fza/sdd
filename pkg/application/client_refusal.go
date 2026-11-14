package application

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// RefuseClient refuses resuming session id through the MCP client clientName
// where its home project, or the project a running move of it targets, needs
// another client. It reads the session's ledger without replaying it, so no
// hook of the session runs or asks for confirmation first.
func (a *Application) RefuseClient(ctx context.Context, identity RequestIdentity, id SessionID, clientName string) error {
	_, runtime, stored, err := a.resolveSession(ctx, identity, id)
	if err != nil {
		return err
	}
	return a.sessionClientRefusal(ctx, identity, runtime, stored.Events, clientName)
}

// RefuseOpen refuses opening a session in project through the MCP client
// clientName where the project needs another client, before any of its hooks
// is asked about.
func (a *Application) RefuseOpen(ctx context.Context, identity RequestIdentity, project ProjectID, clientName string) error {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return err
	}
	return runtime.options.Verifier.clientRefusal(clientName)
}

func (a *Application) sessionClientRefusal(ctx context.Context, identity RequestIdentity, home *ProjectRuntime, events []StoredEvent, clientName string) error {
	err := home.options.Verifier.clientRefusal(clientName)
	if err != nil || !model.CodexClient(clientName) {
		return err
	}
	projects, err := runningInstanceProjects(events)
	if err != nil {
		return err
	}
	principal, err := a.resolvePrincipal(ctx, identity)
	if err != nil {
		return err
	}
	for _, project := range projects {
		if project == home.options.Project.ID {
			continue
		}
		target, err := a.resolveProject(ctx, principal, project, AccessRead)
		if err != nil {
			return err
		}
		if err := target.options.Verifier.clientRefusal(clientName); err != nil {
			return fmt.Errorf("a move of this session targets %s: %w", project, err)
		}
	}
	return nil
}

// runningInstanceProjects lists, in order, the projects the session's
// instances recorded as their targets, leaving out instances that ended.
func runningInstanceProjects(events []StoredEvent) ([]ProjectID, error) {
	targets := map[string]ProjectID{}
	var order []string
	for _, stored := range events {
		switch stored.Code {
		case workflowInstanceProjectCode:
			var event instanceProjectEvent
			if err := json.Unmarshal(stored.Payload, &event); err != nil {
				return nil, fmt.Errorf("decoding instance project event: %w", err)
			}
			if _, seen := targets[event.Instance]; !seen {
				order = append(order, event.Instance)
			}
			targets[event.Instance] = event.Project
		case WorkflowEventCode:
			var event engine.Event
			if err := json.Unmarshal(stored.Payload, &event); err != nil {
				return nil, fmt.Errorf("decoding workflow event: %w", err)
			}
			if event.Event == engine.EventCompleted || event.Event == engine.EventAbandoned {
				delete(targets, event.Instance)
			}
		}
	}
	var projects []ProjectID
	for _, instance := range order {
		if project, ok := targets[instance]; ok && !slices.Contains(projects, project) {
			projects = append(projects, project)
		}
	}
	return projects, nil
}

// refuseClientIn refuses a move into project where the client the session
// last attached through cannot run the deep verifier project's policy needs.
func (w *WorkflowSession) refuseClientIn(ctx context.Context, project ProjectID) error {
	client, err := w.attachedClient(ctx)
	if err != nil {
		return err
	}
	runtime, err := w.targetRuntime(project, AccessRead)
	if err != nil {
		return err
	}
	return runtime.options.Verifier.clientRefusal(client)
}

// attachedClient is the MCP client the session last attached through.
func (w *WorkflowSession) attachedClient(ctx context.Context) (string, error) {
	stored, err := w.app.sessions.Load(ctx, w.binding.SessionID)
	if err != nil {
		return "", err
	}
	if attachment := stored.Metadata.Attachment; attachment != nil {
		return attachment.ClientName, nil
	}
	return "", nil
}
