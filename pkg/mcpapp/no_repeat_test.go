package mcpapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// The no-repeat rule over every verb that returns an entry's metadata, title,
// summary or body (20261109-004506-d-cpt-udz, R14): once an entry was served
// in full in the MCP session, each verb serves it as a pointer (ID, brief
// and the way back) until resume_session resets what the consumer holds.
//
//	verb             serves                         test
//	show             envelope, body, chain lines    TestNoRepeat/show
//	view             rows, bodies, focus, roles     TestNoRepeat/view as-list, view as-bodies
//	search           result lines, citations        TestNoRepeat/search
//	gather           brief lines, relevant bodies   TestGatherPointsAtAnEntryAlreadyServedInFull,
//	                 (explicit ids full)            TestGather_RelevantEntryTheConsumerHoldsPrintsAsItsPointer (application)
//	resume_session   resets the held set            TestNoRepeat/resume_session
//	start_procedure, next: step units and framing lanes, deduplicated as blocks:
//	                 TestCaptureLanesDedupAcrossInstances, TestReportSchemaDedup (serve_dedup_test.go)
//	  entry chains   pointer for a held entry, holds what they serve:
//	                 TestNoRepeatInjectedEntryChains, TestInjectedEntryChains* (application)
//	  views          pointer for a held entry, hold what they serve whole:
//	                 TestNoRepeatInjectedViews, TestInjectedView* (application)
//	  late entries   pointer for a held entry: TestCaptureGrounding_LateEntryHeldPrintsAsItsPointer (proctest)
//	  framing lanes  hold the entries they serve whole, never cut (per-lane dedup):
//	                 TestNoRepeatAfterFramingServedInFull, TestNoRepeatFramingHoldsOnlyWhatItServed,
//	                 TestFramingServesHeldEntriesInFull (application)
//	  fact index, procedure list, recheck items: ID and title or brief rows, no
//	                 more than a pointer carries, deduplicated as lanes; not cut
//	  generated summary and title: the entry the capture just wrote, never served before
//	read_attachment  attachment pages, no entry metadata, out of scope
//	info, registry, park, abandon, bind_branch, stage_attachment, sync_repos:
//	                 no entry metadata, out of scope
//
// The CLI never cuts, with --session too: TestCLIReadsServeInFullEveryTime
// (internal/cliapp).
const pointerMark = "(served in full above)"

func TestNoRepeat(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	pointer := fixtureGapID + " — Pre-flight verdict oscillation produces contradictory findings across runs. " + pointerMark

	var first mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &first)
	if !strings.Contains(first.Entries, "# body") || strings.Contains(first.Entries, pointerMark) {
		t.Fatalf("the first show should serve the entry in full:\n%s", first.Entries)
	}

	t.Run("show", func(t *testing.T) {
		var again mcpserver.ShowResult
		call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &again)
		if !strings.Contains(again.Entries, pointer) || strings.Contains(again.Entries, "# body") {
			t.Fatalf("a repeated show should serve the pointer, not the body:\n%s", again.Entries)
		}
		if !strings.Contains(again.Entries, "gather with its ids, or resume_session") {
			t.Fatalf("the pointer should name the way back:\n%s", again.Entries)
		}
	})
	t.Run("view as-list", func(t *testing.T) {
		var listed mcpserver.ViewResult
		call(t, cs, "view", map[string]any{"session": session, "layout": `topic("testing/fixture"):as-list`}, &listed)
		if !strings.Contains(listed.Sections, pointer) {
			t.Fatalf("a listing should serve a held entry as its pointer:\n%s", listed.Sections)
		}
	})
	t.Run("view as-bodies", func(t *testing.T) {
		var bodies mcpserver.ViewResult
		call(t, cs, "view", map[string]any{"session": session, "layout": `id("` + fixtureGapID + `"):as-bodies`}, &bodies)
		if !strings.Contains(bodies.Sections, pointer) || strings.Contains(bodies.Sections, "on identical input") {
			t.Fatalf("a bodies view should serve a held entry as its pointer:\n%s", bodies.Sections)
		}
	})
	t.Run("search", func(t *testing.T) {
		var found mcpserver.SearchResult
		call(t, cs, "search", map[string]any{"session": session, "terms": []string{"oscillation"}}, &found)
		if !strings.Contains(found.Results, pointer) || strings.Contains(found.Results, "on identical input") {
			t.Fatalf("a search should serve a held hit as its pointer without its citations:\n%s", found.Results)
		}
	})
	t.Run("resume_session", func(t *testing.T) {
		var resumed mcpserver.ResumeSessionResult
		call(t, cs, "resume_session", map[string]any{"session": session}, &resumed)
		var after mcpserver.ShowResult
		call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &after)
		if !strings.Contains(after.Entries, "# body") || strings.Contains(after.Entries, pointerMark) {
			t.Fatalf("after resume_session the entry should serve in full again:\n%s", after.Entries)
		}
	})
}

// A body served by view counts as held, as one served by show does.
func TestNoRepeatAfterViewBodies(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	var bodies mcpserver.ViewResult
	call(t, cs, "view", map[string]any{"session": session, "layout": `id("` + fixtureGapID + `"):as-bodies`}, &bodies)
	var shown mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &shown)
	if !strings.Contains(shown.Entries, pointerMark) || strings.Contains(shown.Entries, "# body") {
		t.Fatalf("a show after a bodies view should serve the pointer:\n%s", shown.Entries)
	}
}

// A second session holds nothing the first was served.
func TestNoRepeatIsPerSession(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	first := openSession(t, cs).Session
	second := openSession(t, cs).Session
	var shown mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": first, "ids": []string{fixtureGapID}}, &shown)
	call(t, cs, "show", map[string]any{"session": second, "ids": []string{fixtureGapID}}, &shown)
	if !strings.Contains(shown.Entries, "# body") {
		t.Fatalf("another session should be served the entry in full:\n%s", shown.Entries)
	}
}

// An entry chain a step serves prints an entry the consumer already holds as
// its pointer, and holds every entry it serves in full.
func TestNoRepeatInjectedEntryChains(t *testing.T) {
	explore := func(t *testing.T, cs *mcp.ClientSession, session string) mcpserver.ServeResult {
		t.Helper()
		var serve mcpserver.ServeResult
		call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "explore", "params": map[string]any{
			"targets": []string{fixtureGapID}, "goal": "what surrounds the oscillation gap",
		}}, &serve)
		return serve
	}
	t.Run("held before the serve", func(t *testing.T) {
		env := newTestServer(t, nil, "", "")
		cs := connect(t, env.srv)
		session := openSession(t, cs).Session
		var shown mcpserver.ShowResult
		call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &shown)
		serve := explore(t, cs, session)
		if !strings.Contains(serve.Instructions, fixtureGapID+" — ") || !strings.Contains(serve.Instructions, pointerMark) || strings.Contains(serve.Instructions, "on identical input") {
			t.Fatalf("the step should serve the held target as its pointer:\n%s", serve.Instructions)
		}
	})
	t.Run("held after the serve", func(t *testing.T) {
		env := newTestServer(t, nil, "", "")
		cs := connect(t, env.srv)
		session := openSession(t, cs).Session
		serve := explore(t, cs, session)
		if !strings.Contains(serve.Instructions, "on identical input") {
			t.Fatalf("the step should serve the target in full:\n%s", serve.Instructions)
		}
		var shown mcpserver.ShowResult
		call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &shown)
		if !strings.Contains(shown.Entries, pointerMark) || strings.Contains(shown.Entries, "# body") {
			t.Fatalf("a show after the step served the entry should serve the pointer:\n%s", shown.Entries)
		}
	})
}

// An entry a framing lane served in full is held: the working principles the
// door serves print as their pointer on a later show.
func TestNoRepeatAfterFramingServedInFull(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	var shown mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{principlesFactID}}, &shown)
	if !strings.Contains(shown.Entries, principlesFactID+" — ") || !strings.Contains(shown.Entries, pointerMark) {
		t.Fatalf("a show of the principles the door served should serve the pointer:\n%s", shown.Entries)
	}
}

// A framing lane holds only the entries it served whole: one the lane's size
// budget left to a pull serves in full on show.
func TestNoRepeatFramingHoldsOnlyWhatItServed(t *testing.T) {
	graphDir := writeFixtureGraph(t)
	served, dropped := "20260811-090000-s-prc-ona", "20260811-090001-s-prc-onb"
	for _, id := range []string{served, dropped} {
		path := filepath.Join(graphDir, "2026/08/"+strings.TrimPrefix(id, "202608")+".md")
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		body := strings.Repeat("**Ask before assuming.** This project's own posture, authored locally at length.\n\n", 80)
		fact := "---\ntype: signal\nlayer: process\nkind: fact\nconfidence: high\ntopics:\n    - principles/interactive\nsummary: A house rule of this project.\n---\n\n# House rules\n\n" + body
		err = os.WriteFile(path, []byte(fact), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	env := newTestServer(t, nil, graphDir, "")
	cs := connect(t, env.srv)
	door := openSession(t, cs)
	if !strings.Contains(door.Framing, "`"+served+"`") || strings.Contains(door.Framing, "`"+dropped+"`") || !strings.Contains(door.Framing, "withheld for size, 1 whole") || !strings.Contains(door.Framing, "- "+dropped+" — ") {
		t.Fatalf("the principles lane should serve one house rule and leave the other to a pull:\n%s", door.Framing)
	}
	var shown mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": door.Session, "ids": []string{served}}, &shown)
	if !strings.Contains(shown.Entries, pointerMark) {
		t.Fatalf("the house rule the lane served should serve the pointer:\n%s", shown.Entries)
	}
	call(t, cs, "show", map[string]any{"session": door.Session, "ids": []string{dropped}}, &shown)
	if strings.Contains(shown.Entries, pointerMark) || !strings.Contains(shown.Entries, "Ask before assuming") {
		t.Fatalf("the house rule the lane left out should serve in full:\n%s", shown.Entries)
	}
}

// viewStepProcedure serves the fixture gap's body through a view its one step
// injects.
const viewStepProcedure = `---
type: decision
kind: procedure
layer: process
canonical: view-step
confidence: high
summary: A workflow whose step serves the fixture gap through a view.
state:
    body: {type: text, desc: what the view showed}
steps:
    - id: look
      inject:
          - {fn: viewLayout, args: {layout: 'id("` + fixtureGapID + `"):as-bodies'}}
      collect: [body]
      transitions:
          - when: hasBody
            to: end(completed)
---

Serves the fixture gap through a view.

## unit: look

{{.viewLayout}}
`

// A view a step injects prints an entry the consumer already holds in full as
// its pointer, and holds every entry it serves in full.
func TestNoRepeatInjectedViews(t *testing.T) {
	t.Run("held before the serve", func(t *testing.T) {
		env := newTestServer(t, nil, "", "")
		cs := connect(t, env.srv)
		session := openSession(t, cs).Session
		var shown mcpserver.ShowResult
		call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &shown)
		var serve mcpserver.ServeResult
		call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "catch-up"}, &serve)
		if !strings.Contains(serve.Instructions, fixtureGapID+" — Pre-flight verdict oscillation produces contradictory findings across runs. "+pointerMark) {
			t.Fatalf("the catch-up listings should serve the held gap as its pointer:\n%s", serve.Instructions)
		}
	})
	t.Run("held after the serve", func(t *testing.T) {
		graphDir := writeFixtureGraph(t)
		path := filepath.Join(graphDir, "2026/07/04-120001-d-prc-vws.md")
		err := os.WriteFile(path, []byte(viewStepProcedure), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		env := newTestServer(t, nil, graphDir, "")
		cs := connect(t, env.srv)
		session := openSession(t, cs).Session
		var serve mcpserver.ServeResult
		call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "view-step"}, &serve)
		if !strings.Contains(serve.Instructions, "on identical input") {
			t.Fatalf("the step should serve the gap in full:\n%s", serve.Instructions)
		}
		var shown mcpserver.ShowResult
		call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &shown)
		if !strings.Contains(shown.Entries, pointerMark) || strings.Contains(shown.Entries, "# body") {
			t.Fatalf("a show after the step served the gap should serve the pointer:\n%s", shown.Entries)
		}
	})
}
