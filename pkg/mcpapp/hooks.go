package mcpapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/presenters"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// multiRoundTripVersion is the first protocol version on which a server asks
// the client for input inside a result's input requests (SEP-2322) instead of
// sending its own request.
const multiRoundTripVersion = "2026-07-28"

// errHookInputRequired stops a call whose hook waits for the person's
// answer, which the client returns on its retry of the call.
var errHookInputRequired = errors.New("waiting for the person to confirm a hook")

// elicitingConfirmer asks the person behind the MCP client, by elicitation,
// whether a hook this machine has not confirmed may run
// (20261109-213250-d-tac-bve). The client shows the prompt to the person,
// not to the agent, so accepting confirms the hook as `sdd hooks trust` does.
// On a multi round-trip protocol the question rides the call's result as an
// input request, and the answer arrives with the client's retry.
type elicitingConfirmer struct {
	session   *mcp.ServerSession
	responses mcp.InputResponseMap
	requests  mcp.InputRequestMap
}

func newElicitingConfirmer(req *mcp.CallToolRequest) *elicitingConfirmer {
	confirmer := &elicitingConfirmer{requests: mcp.InputRequestMap{}}
	if req == nil {
		return confirmer
	}
	confirmer.session = req.Session
	if req.Params != nil {
		confirmer.responses = req.Params.InputResponses
	}
	return confirmer
}

func (c *elicitingConfirmer) ConfirmHook(ctx context.Context, review sdd.HookReview) (bool, error) {
	if c.session == nil {
		return false, errors.New("the call has no client session to ask in")
	}
	params := c.session.InitializeParams()
	if params == nil || params.Capabilities == nil || params.Capabilities.Elicitation == nil {
		return false, errors.New("the MCP client cannot ask the person: it supports no elicitation")
	}
	id := "sdd-hook-" + review.Hook.Hash
	if response, ok := c.responses[id]; ok {
		result, ok := response.(*mcp.ElicitResult)
		if !ok {
			return false, fmt.Errorf("the client answered the hook prompt with %T", response)
		}
		return result.Action == "accept", nil
	}
	request := &mcp.ElicitParams{
		Mode:            "form",
		Message:         "sdd is about to run a hook this machine has not confirmed. Accept to confirm its hash and run it; decline to stop the call.\n\n" + presenters.HookReviewText(review),
		RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}
	if params.ProtocolVersion >= multiRoundTripVersion {
		c.requests[id] = request
		return false, errHookInputRequired
	}
	result, err := c.session.Elicit(ctx, request)
	if err != nil {
		return false, err
	}
	return result.Action == "accept", nil
}

// inputRequired returns the result asking the client for the person's
// answers when err stopped the call for them.
func (c *elicitingConfirmer) inputRequired(err error) (*mcp.CallToolResult, bool) {
	if !errors.Is(err, errHookInputRequired) || len(c.requests) == 0 {
		return nil, false
	}
	return &mcp.CallToolResult{InputRequests: c.requests}, true
}
