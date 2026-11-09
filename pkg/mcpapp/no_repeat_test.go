package mcpapp_test

import (
	"strings"
	"testing"

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
//	gather           brief lines (explicit ids full) TestGatherPointsAtAnEntryAlreadyServedInFull
//	resume_session   resets the held set            TestNoRepeat/resume_session
//	start_procedure, next: step units and framing lanes, deduplicated as blocks:
//	                 TestCaptureLanesDedupAcrossInstances, TestReportSchemaDedup (serve_dedup_test.go)
//	read_attachment  attachment pages, no entry metadata, out of scope
//	info, registry, park, abandon, bind_branch, stage_attachment, sync_repos, start_session:
//	                 no entry metadata, out of scope
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
