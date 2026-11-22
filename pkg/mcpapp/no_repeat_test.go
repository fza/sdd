package mcpapp_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// The no-repeat rule over the verbs that return an entry's metadata, title,
// summary or body: an entry read in full serves in full again on a later
// call, whatever that call answers, until a decision of its own settles how
// a read entry serves again (20261121-230157-d-tac-ym8). Within one result
// an entry served in full above prints as its pointer, and the call after a
// result whose lanes wait points at the entries that result sent.
//
//	verb             later call                     test
//	show, view, search, gather: in full again       TestEntriesServeInFullOnEveryRead
//	start_procedure, next: step lanes, report schemas and framing serve short
//	                 after a right answer           TestTextQuestion* (text_question_test.go)
//	  entry chains   in full again on a later read  TestEntriesAStepServedServeInFullOnALaterRead
//	gather           in full again on a later call  TestGatherServesAnEntryInFullAgainOnALaterCall
//
// The CLI never cuts, with --session too: TestCLIReadsServeInFullEveryTime
// (internal/cliapp).
const pointerMark = "(served in full above"

func TestEntriesServeInFullOnEveryRead(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var first mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &first)
	if !strings.Contains(first.Entries, "# body") || strings.Contains(first.Entries, pointerMark) {
		t.Fatalf("the first show should serve the entry in full:\n%s", first.Entries)
	}

	t.Run("show", func(t *testing.T) {
		var again mcpserver.ShowResult
		call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &again)
		if !strings.Contains(again.Entries, "# body") || strings.Contains(again.Entries, pointerMark) {
			t.Fatalf("a repeated show should serve the entry in full again:\n%s", again.Entries)
		}
	})
	t.Run("view as-bodies", func(t *testing.T) {
		var bodies mcpserver.ViewResult
		call(t, cs, "view", map[string]any{"session": session, "layout": `id("` + fixtureGapID + `"):as-bodies`}, &bodies)
		if !strings.Contains(bodies.Sections, "on identical input") || strings.Contains(bodies.Sections, pointerMark) {
			t.Fatalf("a bodies view should serve the entry in full again:\n%s", bodies.Sections)
		}
	})
	t.Run("search", func(t *testing.T) {
		var found mcpserver.SearchResult
		call(t, cs, "search", map[string]any{"session": session, "terms": []string{"oscillation"}}, &found)
		if strings.Contains(found.Results, pointerMark) {
			t.Fatalf("a search should serve the hit with its citations again:\n%s", found.Results)
		}
	})
}

// An entry chain a step serves in full serves in full again on a later read.
func TestEntriesAStepServedServeInFullOnALaterRead(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "explore", "params": map[string]any{
		"targets": []string{fixtureGapID}, "goal": "what surrounds the oscillation gap",
	}}, &serve)
	if !strings.Contains(serve.Instructions, "on identical input") {
		t.Fatalf("the step should serve the target in full:\n%s", serve.Instructions)
	}
	var shown mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &shown)
	if !strings.Contains(shown.Entries, "# body") || strings.Contains(shown.Entries, pointerMark) {
		t.Fatalf("a show after the step served the entry should serve it in full:\n%s", shown.Entries)
	}
}

// A framing block holds no entry: the working principles the door serves come
// in full again on a show (20261121-233256-d-tac-mm2).
func TestFramingHoldsNoEntry(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	door := openSession(t, cs)
	var shown mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": door.Session, "ids": []string{principlesFactID}}, &shown)
	if strings.Contains(shown.Entries, pointerMark) {
		t.Fatalf("a show of the principles the door served should serve them in full:\n%s", shown.Entries)
	}
}

// Both doors explain the question once: the opening serve and every
// reorientation carry it.
func TestDoorsExplainTheQuestion(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	door := openSession(t, cs)
	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": door.Session}, &resumed)
	for name, text := range map[string]string{"start_session": door.Instructions, "resume_session": resumed.Instructions} {
		if !strings.Contains(text, "must answer both in answer, in their order and parted by ;") || !strings.Contains(text, "List in reread") {
			t.Fatalf("%s should explain how to answer the question and how to have a piece again:\n%s", name, text)
		}
	}
}

// The tools that advance a session require answer; no tool takes held.
func TestAdvancingToolsRequireTheAnswer(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	tools, err := cs.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	advancing := map[string]bool{"start_procedure": true, "next": true, "park": true, "abandon": true}
	for _, tool := range tools.Tools {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var input struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		err = json.Unmarshal(schema, &input)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := input.Properties["held"]; ok {
			t.Errorf("%s should take no held argument", tool.Name)
		}
		answer, ok := input.Properties["answer"]
		if ok != advancing[tool.Name] || ok != slices.Contains(input.Required, "answer") {
			t.Errorf("%s should take a required answer exactly when it advances a session", tool.Name)
		}
		if ok && !strings.Contains(answer.Description, "lost") {
			t.Errorf("%s: the answer field should name lost, got %q", tool.Name, answer.Description)
		}
	}
}
