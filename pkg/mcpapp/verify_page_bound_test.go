package mcpapp_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// A verifier's reading is kept whole and cut into pages as each page is
// served, for the host of the delegate's call, not for the drafter's: the same
// reading takes fewer pages served to Claude Code than to another host, each
// within that host's bound, and a turn or a verdict from a host of another
// page size starts the reading again from page 1.
func TestAVerifierReadingIsPagedForTheCallingHost(t *testing.T) {
	var entries []*model.Entry
	var ids []string
	for n := range 16 {
		var body strings.Builder
		for i := range 40 {
			fmt.Fprintf(&body, "Paragraph %d of entry %d observes how the reading behaves under load, with enough words to fill a line.\n\n", i, n)
		}
		e := &model.Entry{
			ID: fmt.Sprintf("20260601-%06d-s-tac-p%02d", 100000+n, n), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
			Summary: fmt.Sprintf("Entry %d of the paged fixture observes the reading under load.", n),
			Content: body.String(), Topics: proctest.MustTopics("testing/fixture"), Confidence: "medium",
		}
		entries, ids = append(entries, e), append(ids, e.ID)
	}
	entries = append(entries, proctest.RealisticGraph(proctest.DefaultBase(), youngProjectShape)...)
	world := proctest.NewWorld(t, proctest.WithEntries(entries...), proctest.WithVerifier(sdd.VerifierPolicy{Mode: "deep"}, 200000))
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	drafter, _ := boundedConnectNamed(t, srv, "claude-ai", int(model.DefaultResultBound), false)
	session := openSession(t, drafter).Session
	var serve mcpserver.ServeResult
	call(t, drafter, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	capture := serve.Instance
	callRaw(t, drafter, "gather", map[string]any{"session": session, "instance": capture, "ids": ids})
	call(t, drafter, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{
		"body": "A tactical gap: the paged fixture observes the reading under load.", "entryKind": "gap", "layer": "tactical",
		"refs":   []map[string]any{{"id": ids[0], "kind": "related", "desc": "the entry this gap sits beside"}},
		"topics": []string{"testing/fixture"}, "confidence": "medium", "widenReport": "opened every fixture entry",
	}}, &serve)
	call(t, drafter, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"}}, &serve)
	call(t, drafter, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "deepDispatch", "choice": "delegate"}}, &serve)
	if len(serve.Dispatches) != 1 {
		t.Fatalf("the dispatching answer should owe one dispatch, got %+v", serve.Dispatches)
	}
	child, verify := serve.Dispatches[0].Session, serve.Dispatches[0].Instance
	claudeCode, _ := boundedConnectNamed(t, srv, model.ClaudeCodeClient, int(model.ClaudeCodeResultBound), false)
	other, _ := boundedConnectNamed(t, srv, "another-host", int(model.DefaultResultBound), false)

	large, small := firstPageCount(t, claudeCode, child), firstPageCount(t, other, child)
	if large < 2 || large >= small {
		t.Fatalf("the reading should take fewer pages served to Claude Code than to another host, got %d against %d", large, small)
	}
	var page mcpserver.ServeResult
	for turned := 1; turned < large; turned++ {
		page = verifyTurn(t, claudeCode, child, verify, "more")
		if want := fmt.Sprintf("Page %d of %d.", turned+1, large); !strings.Contains(page.Instructions, want) {
			t.Fatalf("each turn should serve the next of Claude Code's pages, want %q in %.300q", want, page.Instructions)
		}
	}
	if !strings.Contains(page.Instructions, "Every page is served.") {
		t.Fatalf("the last page should say every page is served, got %.300q", page.Instructions)
	}

	text, isError := callOutcome(t, other, "next", map[string]any{"session": child, "instance": verify, "report": map[string]any{"chooser": "read", "choice": "judge", "fields": map[string]any{"findings": []any{}}}})
	if !isError || !strings.Contains(text, "pages of another size") {
		t.Fatalf("a verdict from a host of another page size should be refused until its pages are served, got %.300q", text)
	}
	page = verifyTurn(t, other, child, verify, "more")
	if text := page.Instructions; !strings.Contains(text, "starts again") || !strings.Contains(text, fmt.Sprintf("Page 1 of %d.", small)) {
		t.Fatalf("a turn from a host of another page size should start the reading again at its page 1, got %.300q", page.Instructions)
	}
}

var pageNote = regexp.MustCompile(`Page 1 of (\d+)\.`)

// firstPageCount resumes the verifier's session through cs and returns how
// many pages its page 1 names.
func firstPageCount(t *testing.T, cs *mcp.ClientSession, child string) int {
	t.Helper()
	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": child}, &resumed)
	for _, open := range resumed.Open {
		if match := pageNote.FindStringSubmatch(open.Instructions); match != nil {
			count, err := strconv.Atoi(match[1])
			if err != nil {
				t.Fatal(err)
			}
			return count
		}
	}
	t.Fatalf("the resume should serve the reading's page 1, got %+v", resumed.Open)
	return 0
}

// verifyTurn reports choice at the verifier's read step through cs, serving
// any lanes of the result before that wait.
func verifyTurn(t *testing.T, cs *mcp.ClientSession, child, verify, choice string) mcpserver.ServeResult {
	t.Helper()
	var page mcpserver.ServeResult
	call(t, cs, "next", map[string]any{"session": child, "instance": verify, "report": map[string]any{"chooser": "read", "choice": choice}}, &page)
	for n := 0; strings.Contains(page.Instructions, "lanes of these instructions wait"); n++ {
		if n > 5 {
			t.Fatal("the page's waiting lanes should serve within five calls")
		}
		call(t, cs, "next", map[string]any{"session": child, "instance": verify}, &page)
	}
	return page
}
