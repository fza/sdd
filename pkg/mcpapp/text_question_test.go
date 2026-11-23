package mcpapp_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	test "github.com/networkteam/sdd/internal/testsupport"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
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

// Every response that starts, resumes or advances a session ends with two
// questions, no question repeats within the session, and no result carries a
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
		questions := test.Questions(fields.Question)
		if len(questions) != 2 || strings.Count(fields.Question, " | lost") != 2 {
			t.Fatalf("%s: the response should carry two questions, got %q", tool, fields.Question)
		}
		for _, q := range questions {
			if asked[q.Ask] {
				t.Fatalf("%s: a question should never be asked twice in a session, got %q again", tool, q.Ask)
			}
			asked[q.Ask] = true
		}
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
		if strings.Contains(answer, "lost") {
			t.Errorf("call %d: an agent holding every text should answer each question, got %q", i+2, answer)
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
		{"right, swapped", func(right string) string {
			first, second, _ := strings.Cut(right, " ; ")
			return second + ";" + first
		}, true},
		{"one of two lost", func(right string) string {
			first, _, _ := strings.Cut(right, " ; ")
			return first + " ; lost"
		}, false},
		{"one of two given", func(right string) string {
			first, _, _ := strings.Cut(right, " ; ")
			return first
		}, false},
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

// No question asks about what the agent wrote, played back or echoed in a
// lane: a compaction summary quotes it (20261121-230157-d-tac-ym8).
func TestTextQuestionAsksNothingTheAgentWrote(t *testing.T) {
	var entries []*model.Entry
	var refs []map[string]any
	for n := range 3 {
		e := &model.Entry{
			ID: fmt.Sprintf("20260601-%06d-s-tac-q%02d", 100000+n, n), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
			Summary: fmt.Sprintf("Entry %d of the question fixture observes a gap.", n),
			Content: "The question fixture observes a gap.", Topics: proctest.MustTopics("testing/fixture"), Confidence: "medium",
		}
		entries = append(entries, e)
		refs = append(refs, map[string]any{"id": e.ID, "kind": "related", "desc": fmt.Sprintf("entry %d keeps the harbour lights burning past midnight", n)})
	}
	body := "Lanterns along the quay flicker whenever the northern ferry docks late.\n\nEvery harbour pilot logs the tide twice before the morning crossing begins."
	world := proctest.NewWorld(t, proctest.WithEntries(entries...))
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	agent := newAgentMemory()
	cs := connectAs(t, srv, agent)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	capture := serve.Instance
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	callRaw(t, cs, "gather", map[string]any{"session": session, "instance": capture, "ids": ids})
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{
		"body": body, "entryKind": "gap", "layer": "tactical", "refs": refs,
		"topics": []string{"testing/fixture"}, "confidence": "medium", "widenReport": "opened every fixture entry",
	}}, &serve)
	if serve.Step != "playback" {
		t.Fatalf("the draft should reach playback, got %q: %.300q", serve.Step, serve.Instructions)
	}
	for range 3 {
		call(t, cs, "next", map[string]any{"session": session, "instance": capture}, &serve)
	}
	call(t, cs, "resume_session", map[string]any{"session": session}, &serve)

	written := []string{body}
	for _, ref := range refs {
		written = append(written, ref["desc"].(string))
	}
	if len(agent.asked) == 0 {
		t.Fatal("the responses should end with questions")
	}
	for _, line := range agent.asked {
		for _, q := range test.Questions(line) {
			if q.Name == "lane "+types.DraftLane || q.Name == "lane exchange" || q.Name == "lane parked" {
				t.Errorf("no question should ask about a lane the agent wrote, got %q", line)
			}
			for _, text := range written {
				for _, phrase := range quotedPhrases(q.Ask) {
					if strings.Contains(strings.ToLower(text), strings.ToLower(phrase)) {
						t.Errorf("no question's phrase should occur in what the agent reported, got %q", line)
					}
				}
			}
		}
	}
}

// quotedPhrases lists the phrases a question quotes.
func quotedPhrases(ask string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(ask, -1) {
		out = append(out, m[1])
	}
	return out
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
