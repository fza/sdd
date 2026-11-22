package mcpapp_test

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	test "github.com/networkteam/sdd/internal/testsupport"
)

// answeringTools are the tools that advance a session and take its answer.
var answeringTools = map[string]bool{
	"start_procedure": true, "next": true, "abandon": true, "park": true,
}

// agentMemory is one agent context that keeps every text served to it: each
// call that advances a session answers the question that session's last
// response ended with, from the texts that session served, as an agent that
// still sees them would. A fresh connection is a fresh context.
type agentMemory struct {
	mu sync.Mutex
	// texts holds, per session, every string of every result it served.
	texts map[string][]string
	// questions holds, per session, the last question a response ended with.
	questions map[string]string
	// forget drops every text before the next call, as a lossy compaction
	// would.
	forget bool
	// lost answers lost to every question, as an agent that holds nothing.
	lost bool
	// answers records the answer each advancing call carried.
	answers []string
}

func newAgentMemory() *agentMemory {
	return &agentMemory{texts: map[string][]string{}, questions: map[string]string{}}
}

// answer is what the next advancing call on session answers: the words
// looked up in the texts held, or lost.
func (m *agentMemory) answer(session string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.forget {
		m.texts, m.forget = map[string][]string{}, false
	}
	_, ask, ok := test.LastQuestion(m.questions[session])
	if !ok || m.lost {
		return "lost"
	}
	words, ok := test.AnswerQuestion(ask, m.texts[session])
	if !ok {
		return "lost"
	}
	return words
}

// keep takes in a result served to session, or to the session the result
// names when the call named none.
func (m *agentMemory) keep(session string, res *mcp.CallToolResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, content := range res.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		var fields struct {
			Session  string `json:"session"`
			Question string `json:"question"`
		}
		_ = json.Unmarshal([]byte(text.Text), &fields)
		if session == "" {
			session = fields.Session
		}
		if fields.Question != "" {
			m.questions[session] = fields.Question
		}
		for _, s := range resultStrings(text.Text) {
			if s != fields.Question {
				m.texts[session] = append(m.texts[session], s)
			}
		}
	}
}

// resultStrings decodes a JSON result into its string values, the text the
// agent reads; a text that is no JSON is its own one string.
func resultStrings(text string) []string {
	var decoded any
	err := json.Unmarshal([]byte(text), &decoded)
	if err != nil {
		return []string{text}
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			for _, item := range v {
				walk(item)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(decoded)
	return out
}

func (m *agentMemory) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		params, ok := req.GetParams().(*mcp.CallToolParams)
		var session string
		if ok {
			if args, isMap := params.Arguments.(map[string]any); isMap {
				session, _ = args["session"].(string)
				if answeringTools[params.Name] {
					given, own := args["answer"]
					if !own {
						given = m.answer(session)
						withAnswer := make(map[string]any, len(args)+1)
						for k, v := range args {
							withAnswer[k] = v
						}
						withAnswer["answer"] = given
						params.Arguments = withAnswer
					}
					if s, isString := given.(string); isString {
						m.mu.Lock()
						m.answers = append(m.answers, s)
						m.mu.Unlock()
					}
				}
			}
		}
		res, err := next(ctx, method, req)
		result, ok := res.(*mcp.CallToolResult)
		if !ok || err != nil {
			return res, err
		}
		m.keep(session, result)
		return result, nil
	}
}
