package mcpapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/presenters"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// hookInputRequired stops a call whose hooks wait for the person's answers,
// which the client returns on its retry of the call.
type hookInputRequired struct {
	requests mcp.InputRequestMap
}

func (e *hookInputRequired) Error() string {
	return "waiting for the person to confirm the project's hooks"
}

// elicitingConfirmer asks the person behind the MCP client, by elicitation,
// whether the hooks this machine has not confirmed may run
// (20261109-213250-d-tac-bve). The client shows the prompt to the person,
// not to the agent, so accepting confirms the hooks as `sdd hooks trust`
// does. The questions ride the call's result as input requests and the
// answers arrive with the client's retry; for a client on a protocol before
// multi round-trip the SDK elicits them and calls again.
type elicitingConfirmer struct {
	session   *mcp.ServerSession
	responses mcp.InputResponseMap
}

func newElicitingConfirmer(req *mcp.CallToolRequest) *elicitingConfirmer {
	confirmer := &elicitingConfirmer{}
	if req == nil {
		return confirmer
	}
	confirmer.session = req.Session
	if req.Params != nil {
		confirmer.responses = req.Params.InputResponses
	}
	return confirmer
}

func (c *elicitingConfirmer) ConfirmHooks(_ context.Context, reviews []sdd.HookReview) (bool, error) {
	if c.session == nil {
		return false, errors.New("the call has no client session to ask in")
	}
	params := c.session.InitializeParams()
	if params == nil || params.Capabilities == nil || params.Capabilities.Elicitation == nil {
		return false, errors.New("the MCP client cannot ask the person: it supports no elicitation")
	}
	answered := 0
	for _, review := range reviews {
		response, ok := c.responses[hookInputID(review)]
		if !ok {
			continue
		}
		result, ok := response.(*mcp.ElicitResult)
		if !ok {
			return false, fmt.Errorf("the client answered the hook prompt with %T", response)
		}
		if result.Action != "accept" {
			return false, nil
		}
		answered++
	}
	if answered == len(reviews) {
		return true, nil
	}
	requests := mcp.InputRequestMap{}
	for _, review := range reviews {
		requests[hookInputID(review)] = &mcp.ElicitParams{
			Mode:            "form",
			Message:         "sdd is about to run a hook this machine has not confirmed. Accept to confirm its hash and run it; decline to stop the call.\n\n" + presenters.HookReviewText(review),
			RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		}
	}
	return false, &hookInputRequired{requests: requests}
}

func hookInputID(review sdd.HookReview) string {
	return "sdd-hook-" + review.Hook.Hash
}

// inputRequired returns the result asking the client for the person's
// answers when err stopped the call for them.
func inputRequired(err error) (*mcp.CallToolResult, bool) {
	var asking *hookInputRequired
	if !errors.As(err, &asking) {
		return nil, false
	}
	return &mcp.CallToolResult{InputRequests: asking.requests}, true
}

// confirmHooks asks about the unconfirmed hooks of project before the call
// does any work.
func (s *Server) confirmHooks(ctx context.Context, req *mcp.CallToolRequest, project sdd.ProjectID) error {
	return s.app.ConfirmHooks(ctx, s.requestIdentity(req), project, newElicitingConfirmer(req))
}
