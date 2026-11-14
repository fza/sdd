package mcpapp_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// holdNextAtEnd reads the hold-next line a result's text ends with: bare
// tags, comma-separated (20261114-121534-d-tac-qdb).
var holdNextAtEnd = regexp.MustCompile(`,"hold_next":"([0-9a-z]{4}(?:, [0-9a-z]{4})*)"}\s*$`)

func proofOf(t *testing.T, text, id string) model.Proof {
	t.Helper()
	proofs := model.ReadAnchors(text)
	if len(proofs) != 1 {
		t.Fatalf("the %s served in full should carry one tag:\n%s", id, text)
	}
	return proofs[0]
}

func named(line, tag string) bool { return slices.Contains(holdNextTags(line), tag) }

// Every result ends with the hold-next line: at most engine.MaxHoldNext bare
// tags of pieces it served, the step's lanes before its framing, and no
// unproved list. The served instructions ask for exactly those tags.
func TestResultsEndWithTheHoldNextLine(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	memory := newAgentMemory()
	memory.raw = true
	cs := connectAs(t, env.srv, memory)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
	checkToolCall(t, "start_session", res, err)
	text := contentText(res)
	match := holdNextAtEnd.FindStringSubmatch(text)
	if match == nil {
		t.Fatalf("the opening should end with a hold-next line of bare tags, got %.300q", text[max(0, len(text)-300):])
	}
	if strings.Contains(text, `"unproved"`) {
		t.Error("no result should carry the unproved list any more")
	}
	tags := holdNextTags(match[1])
	if len(tags) == 0 || len(tags) > engine.MaxHoldNext {
		t.Fatalf("the line should name 1 to %d tags, got %d", engine.MaxHoldNext, len(tags))
	}
	var door mcpserver.ServeResult
	decodeStructured(t, res, &door)
	lastLane, firstFraming := -1, len(tags)
	for i, tag := range tags {
		mark := model.AnchorMark(tag)
		switch {
		case strings.Contains(door.Instructions, mark):
			lastLane = i
		case strings.Contains(door.Framing, mark) || strings.Contains(door.Vocabulary, mark):
			firstFraming = min(firstFraming, i)
		default:
			t.Errorf("tag %s should name a piece this result served", tag)
		}
	}
	if lastLane < 0 || lastLane > firstFraming {
		t.Errorf("the step's lanes should come before its framing, got %v", tags)
	}
	if !strings.Contains(door.Instructions, "list in held exactly those tags") || strings.Contains(door.Instructions, "every earlier result") {
		t.Errorf("the proof guide should ask for exactly the hold-next tags, got:\n%s", door.Instructions)
	}
}

// An agent listing exactly the tags the last result named gets the step it
// stands at as pointers when the step serves again, also after a read in
// between that served no step.
func TestAgentListingTheHoldNextTagsGetsPointers(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	memory := newAgentMemory()
	memory.holdNext = true
	cs := connectAs(t, env.srv, memory)
	door := openSessionWhole(t, cs)
	var capture mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture"}, &capture)
	followWaitingLanes(t, cs, door.Session, capture)
	var info mcpserver.InfoResult
	call(t, cs, "info", map[string]any{"session": door.Session}, &info)

	want := memory.lastNamed()
	var again mcpserver.ServeResult
	call(t, cs, "next", map[string]any{"session": door.Session, "instance": capture.Instance}, &again)
	if got := memory.lastSent(); len(want) == 0 || !slices.Equal(got, want) {
		t.Fatalf("the agent should list exactly the tags the last result named, sent %v, named %v", got, want)
	}
	if !strings.Contains(again.Instructions, "you hold the step") {
		t.Errorf("the step served again should point at the lanes the agent proved, got %.300q", again.Instructions)
	}
	requireStubReportSchema(t, "capture served again", again.ReportSchema)
}

// A piece served in full again where a proof could have held it joins the
// hold-next lines of its move, here the running capture, until the move
// ends; a piece served once leaves the line with its call.
func TestARepeatedPieceJoinsTheLinesOfItsMove(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	memory := newAgentMemory()
	memory.raw = true
	cs := connectAs(t, env.srv, memory)
	door := openSession(t, cs)
	var capture mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture"}, &capture)

	show := func() model.Proof {
		var shown mcpserver.ShowResult
		call(t, cs, "show", map[string]any{"session": door.Session, "ids": []string{fixtureGapID}, "held": []any{}}, &shown)
		return proofOf(t, shown.Entries, fixtureGapID)
	}
	var info mcpserver.InfoResult
	once := show()
	call(t, cs, "info", map[string]any{"session": door.Session}, &info)
	if named(info.HoldNext, once.Tag) {
		t.Fatalf("an entry served once should leave the line with its call, got %q", info.HoldNext)
	}

	again := show()
	call(t, cs, "info", map[string]any{"session": door.Session}, &info)
	if !named(info.HoldNext, again.Tag) {
		t.Fatalf("the entry served in full again should stay on the lines of the capture, got %q", info.HoldNext)
	}

	var abandoned mcpserver.AbandonResult
	call(t, cs, "abandon", map[string]any{"session": door.Session, "instance": capture.Instance, "reason": "test"}, &abandoned)
	call(t, cs, "info", map[string]any{"session": door.Session}, &info)
	if named(info.HoldNext, again.Tag) {
		t.Errorf("the repeat should leave the line once its move ended, got %q", info.HoldNext)
	}
}

// An entry is served in full again on any call that does not prove it,
// however long: sdd asks no tag before serving it (no tag challenge).
func TestAnUnprovedLongEntryServesInFullAgain(t *testing.T) {
	const longID = "20260602-100000-s-tac-lng"
	body := strings.Repeat("A long gap entry keeps its whole reasoning in one body for the reader. ", 130)
	graphDir := writeFixtureGraph(t)
	path := filepath.Join(graphDir, "2026/06/02-100000-s-tac-lng.md")
	entry := "---\ntype: signal\nlayer: tactical\nkind: gap\nparticipants:\n    - Tester\nconfidence: medium\nsummary: A long gap entry.\n---\n\n" + body + "\n"
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(body) < 7100 {
		t.Fatalf("precondition: the entry should reach the size a tag challenge would pay off from, got %d", len(body))
	}
	env := newTestServer(t, nil, graphDir, "")
	memory := newAgentMemory()
	memory.raw = true
	cs := connectAs(t, env.srv, memory)
	door := openSession(t, cs)

	var shown mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": door.Session, "ids": []string{longID}, "held": []any{}}, &shown)
	first := proofOf(t, shown.Entries, longID)
	call(t, cs, "show", map[string]any{"session": door.Session, "ids": []string{longID}, "held": []any{}}, &shown)
	if !strings.Contains(shown.Entries, "keeps its whole reasoning") || strings.Contains(shown.Entries, "held as") {
		t.Fatalf("an unproved long entry should serve in full again, got %.300q", shown.Entries)
	}
	if again := proofOf(t, shown.Entries, longID); again.Tag == first.Tag {
		t.Errorf("the entry served in full again should carry a fresh tag, got %s twice", again.Tag)
	}
}

// A gather's brief lines carry no tag, so the line never names them; an
// entry gather opens in full is named.
func TestGatherBriefLinesStayOffTheHoldNextLine(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	memory := newAgentMemory()
	memory.raw = true
	cs := connectAs(t, env.srv, memory)
	door := openSession(t, cs)

	var reading mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": door.Session, "held": []any{}}, &reading)
	if !strings.Contains(reading.Serve, fixtureGapID+" (") {
		t.Fatalf("the gather should serve the fixture entry as a brief line:\n%s", reading.Serve)
	}
	opening := door.Instructions + door.Framing + door.Vocabulary
	for _, tag := range holdNextTags(reading.HoldNext) {
		if !strings.Contains(opening, model.AnchorMark(tag)) {
			t.Errorf("a gather serving brief lines only should name no new tag, got %s in %q", tag, reading.HoldNext)
		}
	}

	var opened mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": door.Session, "ids": []string{fixtureGapID}, "held": []any{}}, &opened)
	if proof := proofOf(t, opened.Serve, fixtureGapID); !named(opened.HoldNext, proof.Tag) {
		t.Errorf("an entry gather opened in full should be named, got %q", opened.HoldNext)
	}
}
