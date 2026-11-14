package mcpapp_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
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
	// holdNext lists only the tags the last result's hold_next named, as the
	// served instructions ask (20261114-121534-d-tac-qdb); sent is what the
	// last call listed.
	holdNext bool
	named    []string
	sent     []string
	// lined and unlined name, per call, the tool whose result carried a
	// hold_next field or none.
	lined, unlined []string
}

// guideExampleProof is the worked proof the proof guide shows; its tag was
// never issued, so an agent proving it would only draw a proof notice.
const guideExampleProof = "gn9p:first"

func newAgentMemory() *agentMemory { return &agentMemory{held: map[string]string{}} }

func untagged(res *mcp.CallToolResult) error {
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			text.Text = test.Untagged(text.Text)
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
	err = json.Unmarshal([]byte(test.Untagged(string(raw))), &structured)
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
	m.sent = nil
	for tag, word := range m.held {
		if m.holdNext && !slices.Contains(m.named, tag) {
			continue
		}
		held = append(held, model.Proof{Tag: tag, Word: word}.String())
		m.sent = append(m.sent, tag)
	}
	return held
}

func (m *agentMemory) lastNamed() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Sorted(slices.Values(m.named))
}

func (m *agentMemory) lastSent() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Sorted(slices.Values(m.sent))
}

// results reports the tools whose results carried a hold_next field and
// those whose results carried none, one name per call.
func (m *agentMemory) results() (lined, unlined []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.lined), slices.Clone(m.unlined)
}

func (m *agentMemory) keep(tool string, res *mcp.CallToolResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, content := range res.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		for _, proof := range model.ReadAnchors(text.Text) {
			if proof.String() == guideExampleProof {
				continue
			}
			m.held[proof.Tag] = proof.Word
		}
	}
	if fields, ok := res.StructuredContent.(map[string]any); ok && !res.IsError {
		line, lined := fields["hold_next"].(string)
		m.named = holdNextTags(line)
		if lined {
			m.lined = append(m.lined, tool)
		} else {
			m.unlined = append(m.unlined, tool)
		}
	}
}

// holdNextTags reads the tags a hold-next line names, in order.
func holdNextTags(line string) []string {
	if line == "" {
		return nil
	}
	return strings.Split(line, ", ")
}

func (m *agentMemory) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		params, ok := req.GetParams().(*mcp.CallToolParams)
		tool := ""
		if ok {
			tool = params.Name
		}
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
		m.keep(tool, result)
		if m.raw {
			return res, nil
		}
		return result, untagged(result)
	}
}
