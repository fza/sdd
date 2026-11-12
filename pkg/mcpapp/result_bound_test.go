package mcpapp_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// boundedResults fails the test for every result whose text content, what
// Claude Code counts, exceeds the result bound.
type boundedResults struct {
	t       *testing.T
	mu      sync.Mutex
	largest map[string]int
}

func (b *boundedResults) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		result, ok := res.(*mcp.CallToolResult)
		if !ok || err != nil {
			return res, err
		}
		chars := 0
		for _, content := range result.Content {
			if text, ok := content.(*mcp.TextContent); ok {
				chars += len(text.Text)
			}
		}
		tool := req.GetParams().(*mcp.CallToolParams).Name
		b.mu.Lock()
		b.largest[tool] = max(b.largest[tool], chars)
		b.mu.Unlock()
		if chars > model.MaxResultChars {
			b.t.Errorf("a %s result of %d characters exceeds the result bound", tool, chars)
		}
		return res, nil
	}
}

// largeGraph writes a graph of many entries with long bodies, one entry
// several times a result's size, and an attachment as large.
func largeGraph(t *testing.T) (dir string, ids []string, huge string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "graph")
	paragraph := func(entry, i int) string {
		return fmt.Sprintf("Paragraph %d of entry %d observes how the fixture behaves under load, with enough words to fill a line.", i, entry)
	}
	for n := range 80 {
		var body strings.Builder
		for i := range 30 {
			body.WriteString(paragraph(n, i) + "\n\n")
		}
		e := &model.Entry{
			ID: fmt.Sprintf("20260601-%06d-s-tac-l%02d", 100000+n, n), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
			Summary: fmt.Sprintf("Entry %d of the large fixture observes the fixture under load, in a summary of some length.", n),
			Content: body.String(), Topics: proctest.MustTopics("testing/fixture"), Confidence: "medium",
		}
		proctest.WriteEntry(t, dir, e)
		ids = append(ids, e.ID)
	}
	var body strings.Builder
	for i := range 2000 {
		body.WriteString(paragraph(99, i) + "\n\n")
	}
	huge = "20260602-100000-s-tac-hug"
	proctest.WriteEntry(t, dir, &model.Entry{
		ID: huge, Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Summary: "An entry several times a result's size.", Content: body.String(), Topics: proctest.MustTopics("testing/fixture"), Confidence: "medium",
	})
	attachments := filepath.Join(dir, "2026/06/02-100000-s-tac-hug")
	if err := os.MkdirAll(attachments, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attachments, "record.md"), []byte(body.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return dir, ids, huge
}

// No result exceeds the result bound (20261112-075604-d-tac-t4r) on a graph
// whose reads would each run far past it: what does not fit pages with a
// continuation the agent follows.
func TestNoResultExceedsTheResultBound(t *testing.T) {
	dir, ids, huge := largeGraph(t)
	cs, bound := boundedClient(t, dir)
	session := openSession(t, cs).Session
	pages := func(tool string, args map[string]any, text func(map[string]any) string) {
		t.Helper()
		page := callRaw(t, cs, tool, args)
		for n := 2; strings.Contains(text(page), "for the next;"); n++ {
			if n > 20 || !strings.Contains(text(page), fmt.Sprintf("call %s again with the same arguments and page %d", tool, n)) {
				t.Fatalf("a paged %s result should name the call for its next page, got %.200q", tool, text(page))
			}
			args["page"] = n
			page = callRaw(t, cs, tool, args)
		}
	}
	field := func(name string) func(map[string]any) string {
		return func(result map[string]any) string { s, _ := result[name].(string); return s }
	}
	pages("view", map[string]any{"session": session, "layout": "active:as-list"}, field("sections"))
	pages("show", map[string]any{"session": session, "ids": ids[:30]}, field("entries"))
	pages("show", map[string]any{"session": session, "ids": []string{huge}}, field("entries"))
	pages("gather", map[string]any{"session": session, "ids": ids[30:50]}, field("serve"))
	pages("search", map[string]any{"session": session, "terms": []string{"fixture"}, "limit": 200}, field("results"))
	var registry mcpserver.RegistryResult
	call(t, cs, "registry", map[string]any{"session": session}, &registry)
	for registry.NextPage != 0 {
		call(t, cs, "registry", map[string]any{"session": session, "page": registry.NextPage}, &registry)
	}
	attachment := mcpserver.ReadAttachmentResult{More: true}
	for read := 0; attachment.More; read += len(attachment.Content) {
		call(t, cs, "read_attachment", map[string]any{"session": session, "id": huge, "name": "record.md", "offset": read}, &attachment)
	}
	var capture mcpserver.ServeResult
	for range 3 {
		call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &capture)
	}
	var gathered mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": session, "instance": capture.Instance}, &gathered)
	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": session}, &resumed)
	t.Logf("largest results: %v", bound.largest)
}

// boundedClient connects to a server over graphDir through a client that
// lists its proofs and fails the test on every result past the bound.
func boundedClient(t *testing.T, graphDir string) (*mcp.ClientSession, *boundedResults) {
	t.Helper()
	return boundedConnect(t, newTestServer(t, nil, graphDir, "").srv, true)
}

// boundedConnect connects to srv through a client that fails the test on
// every result past the bound, listing its proofs when proofs is set.
func boundedConnect(t *testing.T, srv *mcpserver.Server, proofs bool) (*mcp.ClientSession, *boundedResults) {
	t.Helper()
	bound := &boundedResults{t: t, largest: map[string]int{}}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), serverTransport); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	if proofs {
		client.AddSendingMiddleware(newAgentMemory().middleware)
	}
	client.AddSendingMiddleware(bound.middleware)
	cs, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, bound
}

// A step whose lanes together run past the result bound serves the lanes
// that fit and names the rest, which next with no report serves.
func TestAServePastTheResultBoundLeavesItsLastLanesWaiting(t *testing.T) {
	cs, _ := boundedClient(t, writeWideStepGraph(t, 500, "first", "second", "third"))
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "wide-step"}, &serve)
	requireWaitingLane(t, serve)
	var rest mcpserver.ServeResult
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance}, &rest)
	if !strings.Contains(rest.Instructions, "Lane third holds") || strings.Contains(rest.Instructions, "lanes of these instructions wait") {
		t.Fatalf("next with no report should serve the waiting lanes, got %.300q", rest.Instructions)
	}
}

// An agent that lists no proofs reaches every lane of a step past the result
// bound, on one server and across restarts after the first serve and after
// the last follow-up: each follow-up serves waiting lanes and points at every
// lane sent before it, so three lanes take two calls. Four lanes too wide to
// share a result take one call each. The deferral ends with the follow-up
// that serves the last lane, and the step advances on its report.
func TestANoProofAgentReachesEveryWaitingLane(t *testing.T) {
	for _, tc := range []struct {
		lines              int
		lanes              []string
		minCalls, maxCalls int
	}{
		{500, []string{"first", "second", "third"}, 2, 2},
		{650, []string{"first", "second", "third", "fourth"}, 4, 4},
	} {
		for _, restart := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d lanes, restart=%v", len(tc.lanes), restart), func(t *testing.T) {
				env := newTestServer(t, nil, writeWideStepGraph(t, tc.lines, tc.lanes...), "")
				cs, _ := boundedConnect(t, env.srv, false)
				session := openSession(t, cs).Session
				var serve mcpserver.ServeResult
				call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "wide-step"}, &serve)
				requireWaitingLane(t, serve)
				if restart {
					cs, _ = boundedConnect(t, newTestServer(t, nil, env.graphDir, env.sessionsDir).srv, false)
				}
				served := map[string]int{}
				for calls := 1; ; calls++ {
					for _, name := range tc.lanes {
						if strings.Contains(serve.Instructions, "Lane "+name+" holds") {
							served[name]++
						} else if served[name] > 0 && !strings.Contains(serve.Instructions, "lane "+name) {
							t.Fatalf("call %d should point at lane %s, sent before, got %.600q", calls, name, serve.Instructions)
						}
					}
					if !strings.Contains(serve.Instructions, "lanes of these instructions wait") {
						if calls < tc.minCalls || calls > tc.maxCalls {
							t.Fatalf("the lanes should take %d to %d calls, took %d", tc.minCalls, tc.maxCalls, calls)
						}
						break
					}
					if calls == tc.maxCalls {
						t.Fatalf("call %d should serve the last waiting lanes, got %.300q", calls, serve.Instructions)
					}
					call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance}, &serve)
				}
				for _, name := range tc.lanes {
					if served[name] != 1 {
						t.Fatalf("lane %s should serve in full exactly once, served %d times", name, served[name])
					}
				}
				if restart {
					cs, _ = boundedConnect(t, newTestServer(t, nil, env.graphDir, env.sessionsDir).srv, false)
				}
				var again mcpserver.ServeResult
				call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance}, &again)
				if !strings.Contains(again.Instructions, "Lane first holds") {
					t.Fatalf("a call after the deferral ended should serve the lanes in full again, got %.300q", again.Instructions)
				}
				var done mcpserver.ServeResult
				call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "report": map[string]any{"body": "what the step yields"}}, &done)
				if done.Status != "completed" {
					t.Fatalf("the step should advance on its report, got %s at %q", done.Status, done.Step)
				}
			})
		}
	}
}

func requireWaitingLane(t *testing.T, serve mcpserver.ServeResult) {
	t.Helper()
	if !strings.Contains(serve.Instructions, "Lane first holds") || strings.Contains(serve.Instructions, "Lane third holds") || !strings.Contains(serve.Instructions, "lanes of these instructions wait") {
		t.Fatalf("the serve should hold the lanes that fit and name the waiting ones, got %.300q", serve.Instructions)
	}
}

// writeWideStepGraph writes the fixture graph with a procedure whose one step
// serves a lane of lines lines for each name; 500 lines run about 21,500
// characters, so three such lanes run past what one result holds.
func writeWideStepGraph(t *testing.T, lines int, names ...string) string {
	t.Helper()
	graphDir := writeFixtureGraph(t)
	var lanes strings.Builder
	for _, name := range names {
		fmt.Fprintf(&lanes, "### lane: %s\n\n%s\n\n", name, strings.Repeat("Lane "+name+" holds a line of its guidance.\n", lines))
	}
	spec := `---
type: decision
kind: procedure
layer: process
canonical: wide-step
confidence: high
summary: A workflow whose one step serves wide lanes.
state:
    body: {type: text, desc: what the step yields}
steps:
    - id: wide
      collect: [body]
      transitions:
          - when: hasBody
            to: end(completed)
---

Serves wide lanes.

## unit: wide

` + lanes.String()
	if err := os.WriteFile(filepath.Join(graphDir, "2026/07/04-120002-d-prc-wid.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	return graphDir
}

// A resume whose moves would together run past the result bound serves the
// moves that fit and names the others, each resumed through next.
func TestResumeOmitsMovesPastTheResultBound(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	big := strings.Repeat("z", 20000)
	for i := range 6 {
		report := assembleReport()
		report["body"] = fmt.Sprintf("body %d: %s", i, big)
		report["widenReport"] = fmt.Sprintf("widen %d: %s", i, big)
		var serve mcpserver.ServeResult
		call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture", "params": map[string]any{"anchor": fixtureGapID}}, &serve)
		call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "report": report}, &serve)
	}
	res, err := connect(t, env.srv).CallTool(t.Context(), &mcp.CallToolParams{Name: "resume_session", Arguments: map[string]any{"session": session}})
	if err != nil {
		t.Fatal(err)
	}
	text := contentText(res)
	if len(text) > model.MaxResultChars {
		t.Fatalf("the resume should stay within the result bound, got %d characters", len(text))
	}
	if !strings.Contains(text, "omitted here for size") || !strings.Contains(text, "capture at ") {
		t.Fatalf("the moves past the bound should be named, got %.300q", text)
	}
}
