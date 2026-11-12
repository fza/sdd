package mcpapp_test

import (
	"context"
	"encoding/json"
	"regexp"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
)

// provingTools are the tools that take held and reread.
var provingTools = map[string]bool{
	"start_procedure": true, "next": true, "abandon": true, "park": true,
	"search": true, "view": true, "show": true, "gather": true,
}

// agentMemory is one agent context that keeps every piece served to it: each
// call it makes lists every tag it received, with the word after it, as held
// (20261112-075604-d-tac-t4r). A fresh connection is a fresh context. It
// hands the test each result with the placed tags taken out, so assertions
// read the text as written; raw keeps them.
type agentMemory struct {
	mu   sync.Mutex
	held map[string]string
	// forget drops every proof before the next call, as a lossy compaction
	// would.
	forget bool
	raw    bool
}

func newAgentMemory() *agentMemory { return &agentMemory{held: map[string]string{}} }

var placedTag = regexp.MustCompile(`⟦[0-9a-z]{4}⟧ (\pL)`)

func untagged(res *mcp.CallToolResult) error {
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			text.Text = placedTag.ReplaceAllString(text.Text, "$1")
		}
	}
	if res.StructuredContent == nil {
		return nil
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return err
	}
	var structured any
	err = json.Unmarshal(placedTag.ReplaceAll(raw, []byte("$1")), &structured)
	if err != nil {
		return err
	}
	res.StructuredContent = structured
	return nil
}

func (m *agentMemory) proofs() []any {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.forget {
		m.held, m.forget = map[string]string{}, false
	}
	held := make([]any, 0, len(m.held))
	for tag, word := range m.held {
		held = append(held, model.Proof{Tag: tag, Word: word}.String())
	}
	return held
}

func (m *agentMemory) keep(res *mcp.CallToolResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, content := range res.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		for _, proof := range model.ReadAnchors(text.Text) {
			m.held[proof.Tag] = proof.Word
		}
	}
}

func (m *agentMemory) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		params, ok := req.GetParams().(*mcp.CallToolParams)
		if ok && provingTools[params.Name] {
			if args, ok := params.Arguments.(map[string]any); ok {
				if _, own := args["held"]; !own {
					withHeld := make(map[string]any, len(args)+1)
					for k, v := range args {
						withHeld[k] = v
					}
					withHeld["held"] = m.proofs()
					params.Arguments = withHeld
				}
			}
		}
		res, err := next(ctx, method, req)
		result, ok := res.(*mcp.CallToolResult)
		if !ok || err != nil {
			return res, err
		}
		m.keep(result)
		if m.raw {
			return res, nil
		}
		return result, untagged(result)
	}
}
