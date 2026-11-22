package mcpapp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	test "github.com/networkteam/sdd/internal/testsupport"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// lastKey returns the last key of a JSON object as its text carries it.
func lastKey(t *testing.T, text string) string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(text))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	var last string
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		last, _ = key.(string)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return last
}

// Every response that starts, resumes or advances a session ends with one
// question, no question repeats within the session, and no result carries a
// mark placed for the check or a field of the anchor proof.
func TestTextQuestionEndsEveryAdvancingResponse(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	agent := newAgentMemory()
	cs := connectAs(t, env.srv, agent)
	asked := map[string]bool{}
	check := func(tool string, res *mcp.CallToolResult) {
		t.Helper()
		text := contentText(res)
		if lastKey(t, text) != "question" {
			t.Fatalf("%s: the response should end with its question, got %.300s", tool, text)
		}
		var fields struct {
			Question string `json:"question"`
		}
		if err := json.Unmarshal([]byte(text), &fields); err != nil {
			t.Fatal(err)
		}
		if _, _, ok := test.LastQuestion(fields.Question); !ok || strings.Count(fields.Question, " | lost") != 1 {
			t.Fatalf("%s: the response should carry exactly one question, got %q", tool, fields.Question)
		}
		if asked[fields.Question] {
			t.Fatalf("%s: a question should never be asked twice in a session, got %q again", tool, fields.Question)
		}
		asked[fields.Question] = true
		for _, gone := range []string{"⟦", `"unproved"`, `"proof_notice"`} {
			if strings.Contains(text, gone) {
				t.Fatalf("%s: no result should carry %s", tool, gone)
			}
		}
	}
	raw := func(tool string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
		checkToolCall(t, tool, res, err)
		check(tool, res)
		return res
	}
	var door mcpserver.ServeResult
	decodeStructured(t, raw("start_session", map[string]any{}), &door)
	var serve mcpserver.ServeResult
	decodeStructured(t, raw("start_procedure", map[string]any{"session": door.Session, "canonical": "capture"}), &serve)
	raw("next", map[string]any{"session": door.Session, "instance": serve.Instance})
	raw("park", map[string]any{"session": door.Session, "instance": serve.Instance, "note": "a pause"})
	raw("resume_session", map[string]any{"session": door.Session})
	raw("next", map[string]any{"session": door.Session, "instance": serve.Instance})
	raw("abandon", map[string]any{"session": door.Session, "instance": serve.Instance, "reason": "done here"})
	for i, answer := range agent.answers[1:] {
		if answer == "lost" {
			t.Errorf("call %d: an agent holding every text should answer each question, got lost", i+2)
		}
	}
}

// A call that advances a session without the answer is refused with a
// message naming it; lost is a valid answer.
func TestTextQuestionAnswerIsRequired(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connectAs(t, env.srv, newAgentMemory())
	session := openSession(t, cs).Session
	bare := connectBare(t, env.srv)
	for _, args := range []map[string]any{
		{"session": session, "canonical": "capture"},
		{"session": session, "canonical": "capture", "answer": ""},
	} {
		msg := callExpectError(t, bare, "start_procedure", args)
		if !strings.Contains(msg, "answer") {
			t.Errorf("the refusal should name the missing answer, got %q", msg)
		}
	}
	var serve mcpserver.ServeResult
	call(t, bare, "start_procedure", map[string]any{"session": session, "canonical": "capture", "answer": "lost"}, &serve)
	if serve.Step == "" {
		t.Fatal("lost should be a valid answer")
	}
}

// A right answer serves the repeated step text and report schema short; a
// wrong answer or lost serves them in full. The framing goes to the caller
// once per change whatever the answer (20261121-233256-d-tac-mm2).
func TestTextQuestionAnswerDecidesShortOrFull(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(right string) string
		short  bool
	}{
		{"right", func(right string) string { return right }, true},
		{"wrong", func(string) string { return "misread words" }, false},
		{"lost", func(string) string { return "lost" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestServer(t, nil, "", "")
			agent := newAgentMemory()
			cs := connectAs(t, env.srv, agent)
			door := openSession(t, cs)
			var first mcpserver.ServeResult
			call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture"}, &first)
			requireFullReportSchema(t, "first capture", first.ReportSchema)

			var second mcpserver.ServeResult
			call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture", "answer": tc.answer(agent.answer(door.Session))}, &second)
			if tc.short {
				requireStubReportSchema(t, "capture after a right answer", second.ReportSchema)
				if !strings.Contains(second.Instructions, "you hold the step") || !onlyHeldLine(second.Framing) {
					t.Fatalf("a right answer should serve the step text and framing short, got %.400q", second.Instructions)
				}
				return
			}
			requireFullReportSchema(t, "capture after "+tc.name, second.ReportSchema)
			if !strings.Contains(second.Instructions, "Existing topics:") {
				t.Fatalf("%s should serve the step text in full, got %.400q", tc.name, second.Instructions)
			}
			if strings.Contains(second.Framing, "## Working principles") {
				t.Fatalf("%s should not serve the unchanged framing again, got %.400q", tc.name, second.Framing)
			}
		})
	}
}

// connectBare connects a client that sends its arguments as given.
func connectBare(t *testing.T, srv *mcpserver.Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), serverTransport); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "bare-client", Version: "test"}, nil).Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
