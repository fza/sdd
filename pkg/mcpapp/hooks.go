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
	return "waiting for the person to allow the project's hooks"
}

// elicitingAllower asks the person behind the MCP client, by elicitation,
// whether the hooks of the repositories this machine has not allowed may
// run. The client shows the prompt to the person, not to the agent, so
// accepting allows the hooks as `sdd hooks allow` does. The questions ride
// the call's result as input requests and the answers arrive with the
// client's retry; for a client on a protocol before multi round-trip the SDK
// elicits them and calls again.
type elicitingAllower struct {
	session   *mcp.ServerSession
	responses mcp.InputResponseMap
}

func newElicitingAllower(req *mcp.CallToolRequest) *elicitingAllower {
	allower := &elicitingAllower{}
	if req == nil {
		return allower
	}
	allower.session = req.Session
	if req.Params != nil {
		allower.responses = req.Params.InputResponses
	}
	return allower
}

func (c *elicitingAllower) AllowHooks(_ context.Context, reviews []sdd.HookReview) (bool, error) {
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
			Message:         "sdd is about to run hooks that came with a repository and are not allowed on this machine as they are now. Accept to allow them and run them; decline to stop the call.\n\n" + presenters.HookReviewText(review),
			RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		}
	}
	return false, &hookInputRequired{requests: requests}
}

// hookInputID names a review's question by the hash it allows, so an answer
// never allows hooks other than those the person was shown.
func hookInputID(review sdd.HookReview) string {
	return "sdd-hooks-" + review.Hash
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

// allowHooks asks about the hooks of project this machine has not allowed
// before the call does any work.
func (s *toolSet) allowHooks(ctx context.Context, req *mcp.CallToolRequest, project sdd.ProjectID) error {
	return s.app.AllowHooks(ctx, s.requestIdentity(req), project, newElicitingAllower(req))
}
