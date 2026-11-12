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
	sdd "github.com/networkteam/sdd/pkg/application"
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
	cs, _ := boundedClient(t, writeWideStepGraph(t, "first", "second", "third"))
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
// lane sent before it, so three lanes take two calls. The deferral ends with
// the follow-up that serves the last lane, and the step advances on its
// report.
func TestANoProofAgentReachesEveryWaitingLane(t *testing.T) {
	for _, tc := range []struct {
		lanes              []string
		minCalls, maxCalls int
	}{
		{[]string{"first", "second", "third"}, 2, 2},
		{[]string{"first", "second", "third", "fourth"}, 3, 4},
	} {
		for _, restart := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d lanes, restart=%v", len(tc.lanes), restart), func(t *testing.T) {
				env := newTestServer(t, nil, writeWideStepGraph(t, tc.lanes...), "")
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

// followWaitingLanes calls next with no report on a serve whose lanes wait
// until none does, and returns the instructions of every call joined.
func followWaitingLanes(t *testing.T, cs *mcp.ClientSession, session string, serve mcpserver.ServeResult) string {
	t.Helper()
	text := serve.Instructions
	for range 10 {
		if !strings.Contains(serve.Instructions, "lanes of these instructions wait") {
			return text
		}
		call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance}, &serve)
		text += "\n\n" + serve.Instructions
	}
	t.Fatalf("the waiting lanes should serve within 10 calls, got %.300q", serve.Instructions)
	return ""
}

// A lane larger than one result never waits forever: once nothing else fits
// beside it, it serves alone past the bound, so each call serves a lane, as
// the first lane of a step and as one left waiting behind a lane that fit.
func TestAnOversizedLaneServesAloneRatherThanWaiting(t *testing.T) {
	huge := stepLane{"huge", model.MaxResultChars * 6 / 5 / 42}
	small := stepLane{"small", 20}
	for _, tc := range []struct {
		name  string
		lanes []stepLane
	}{
		{"first", []stepLane{huge, small}},
		{"behind a lane that fit", []stepLane{small, huge}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestServer(t, nil, writeLaneStepGraph(t, tc.lanes...), "")
			cs := connect(t, env.srv)
			session := openSession(t, cs).Session
			var serve mcpserver.ServeResult
			call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "wide-step"}, &serve)
			served := map[string]bool{}
			for calls := 1; calls <= len(tc.lanes); calls++ {
				fresh := 0
				for _, lane := range tc.lanes {
					if strings.Contains(serve.Instructions, "Lane "+lane.name+" holds") {
						served[lane.name] = true
						fresh++
					}
				}
				if fresh == 0 {
					t.Fatalf("call %d should serve a lane while lanes wait, got %.300q", calls, serve.Instructions)
				}
				if !strings.Contains(serve.Instructions, "lanes of these instructions wait") {
					break
				}
				call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance}, &serve)
			}
			if !served["huge"] || !served["small"] {
				t.Fatalf("every lane should be served within as many calls as there are lanes, served %v", served)
			}
		})
	}
}

func requireWaitingLane(t *testing.T, serve mcpserver.ServeResult) {
	t.Helper()
	if !strings.Contains(serve.Instructions, "Lane first holds") || strings.Contains(serve.Instructions, "Lane third holds") || !strings.Contains(serve.Instructions, "lanes of these instructions wait") {
		t.Fatalf("the serve should hold the lanes that fit and name the waiting ones, got %.300q", serve.Instructions)
	}
}

// writeWideStepGraph writes the fixture graph with a procedure whose one step
// serves a lane of about half the result bound less 7,000 characters for
// each name: two lanes and the step around them fill one result, three or
// more run past it.
func writeWideStepGraph(t *testing.T, names ...string) string {
	t.Helper()
	lanes := make([]stepLane, len(names))
	for i, name := range names {
		lanes[i] = stepLane{name, (model.MaxResultChars - 7000) / 86}
	}
	return writeLaneStepGraph(t, lanes...)
}

// stepLane is one lane of the wide-step procedure: its name and how many
// lines of about 43 characters it holds.
type stepLane struct {
	name  string
	lines int
}

// writeLaneStepGraph writes the fixture graph with a procedure whose one step
// serves the given lanes.
func writeLaneStepGraph(t *testing.T, laneSizes ...stepLane) string {
	t.Helper()
	graphDir := writeFixtureGraph(t)
	var lanes strings.Builder
	for _, lane := range laneSizes {
		fmt.Fprintf(&lanes, "### lane: %s\n\n%s\n\n", lane.name, strings.Repeat("Lane "+lane.name+" holds a line of its guidance.\n", lane.lines))
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
	big := strings.Repeat("z", model.MaxResultChars*2/5)
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

// Every page of the deep verifier's reading serves in one result within the
// bound: the page and the step around it, no lane left waiting.
func TestEveryDeepReadingPageServesWithinTheResultBound(t *testing.T) {
	var entries []*model.Entry
	var ids []string
	for n := range 12 {
		var body strings.Builder
		for i := range 40 {
			fmt.Fprintf(&body, "Paragraph %d of entry %d observes how the reading behaves under load, with enough words to fill a line.\n\n", i, n)
		}
		e := &model.Entry{
			ID: fmt.Sprintf("20260601-%06d-s-tac-d%02d", 100000+n, n), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
			Summary: fmt.Sprintf("Entry %d of the deep fixture observes the reading under load.", n),
			Content: body.String(), Topics: proctest.MustTopics("testing/fixture"), Confidence: "medium",
		}
		entries, ids = append(entries, e), append(ids, e.ID)
	}
	world := proctest.NewWorld(t, proctest.WithEntries(entries...), proctest.WithVerifier(sdd.VerifierPolicy{Mode: "deep"}, 200000, 4096))
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	cs, bound := boundedConnect(t, srv, true)
	defer func() { t.Logf("largest results: %v", bound.largest) }()
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	capture := serve.Instance
	callRaw(t, cs, "gather", map[string]any{"session": session, "instance": capture, "ids": ids})
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{
		"body": "A tactical gap: the deep fixture observes the reading under load.", "entryKind": "gap", "layer": "tactical",
		"refs":   []map[string]any{{"id": ids[0], "kind": "related", "desc": "the entry this gap sits beside"}},
		"topics": []string{"testing/fixture"}, "confidence": "medium", "widenReport": "opened every fixture entry",
	}}, &serve)
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"}}, &serve)
	if serve.Step != "deepDispatch" {
		t.Fatalf("the confirmed draft should reach the deep dispatch, got %q: %.300q", serve.Step, serve.Instructions)
	}
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "deepDispatch", "choice": "delegate"}}, &serve)
	if len(serve.Dispatches) != 1 {
		t.Fatalf("the dispatching answer should owe one dispatch, got %+v", serve.Dispatches)
	}
	child, verify := serve.Dispatches[0].Session, serve.Dispatches[0].Instance
	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": child}, &resumed)
	page := resumed.Open[0]
	for n := 1; ; n++ {
		if !strings.Contains(page.Instructions, fmt.Sprintf("Page %d of ", n)) || strings.Contains(page.Instructions, "lanes of these instructions wait") {
			t.Fatalf("page %d should serve whole in one result, got %.300q", n, page.Instructions)
		}
		if strings.Contains(page.Instructions, "Every page is served.") {
			if n < 3 {
				t.Fatalf("the reading should run over several pages, it took %d", n)
			}
			return
		}
		if n > 40 {
			t.Fatal("the reading should end within 40 pages")
		}
		call(t, cs, "next", map[string]any{"session": child, "instance": verify, "report": map[string]any{"chooser": "read", "choice": "more"}}, &page)
	}
}
