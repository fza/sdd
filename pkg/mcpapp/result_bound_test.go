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
	env := newTestServer(t, nil, graphDir, "")
	bound := &boundedResults{t: t, largest: map[string]int{}}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := env.srv.Connect(t.Context(), serverTransport); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	client.AddSendingMiddleware(newAgentMemory().middleware, bound.middleware)
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
	graphDir := writeFixtureGraph(t)
	lane := func(name string) string {
		return fmt.Sprintf("### lane: %s\n\n%s\n\n", name, strings.Repeat("Lane "+name+" holds a line of its guidance.\n", 500))
	}
	spec := `---
type: decision
kind: procedure
layer: process
canonical: wide-step
confidence: high
summary: A workflow whose one step serves three wide lanes.
state:
    body: {type: text, desc: what the step yields}
steps:
    - id: wide
      collect: [body]
      transitions:
          - when: hasBody
            to: end(completed)
---

Serves three wide lanes.

## unit: wide

` + lane("first") + lane("second") + lane("third")
	if err := os.WriteFile(filepath.Join(graphDir, "2026/07/04-120002-d-prc-wid.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	cs, _ := boundedClient(t, graphDir)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "wide-step"}, &serve)
	if !strings.Contains(serve.Instructions, "Lane first holds") || strings.Contains(serve.Instructions, "Lane third holds") || !strings.Contains(serve.Instructions, "lanes of these instructions wait") {
		t.Fatalf("the serve should hold the lanes that fit and name the waiting ones, got %.300q", serve.Instructions)
	}
	var rest mcpserver.ServeResult
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance}, &rest)
	if !strings.Contains(rest.Instructions, "Lane third holds") || strings.Contains(rest.Instructions, "lanes of these instructions wait") {
		t.Fatalf("next with no report should serve the waiting lanes, got %.300q", rest.Instructions)
	}
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
