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
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
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
	memory.raw, memory.model = true, opusModel
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
	if !strings.Contains(door.Instructions, "held on every call: exactly those tags") || strings.Contains(door.Instructions, "every earlier result") {
		t.Errorf("the proof guide should ask for exactly the hold-next tags, got:\n%s", door.Instructions)
	}
}

// Only an agent reporting an Opus or Fable model gets the hold-next line:
// every result it gets carries one, whatever its client, while an agent on
// any other model, or reporting none, gets none on any result and proves
// every tag it still sees (20261112-075604-d-tac-t4r), which still makes a
// step served again its pointers.
func TestOnlyAnOpusOrFableAgentGetsTheHoldNextLine(t *testing.T) {
	for _, tc := range []struct {
		name, client, model string
		holdNext            bool
	}{
		{"opus", claudeClient, opusModel, true},
		{"fable", otherClient, fableModel, true},
		{"sonnet", claudeClient, sonnetModel, false},
		{"no model", claudeClient, "", false},
		{"codex", codexClient, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestServer(t, nil, "", "")
			memory := newAgentMemory()
			memory.holdNext, memory.model = tc.holdNext, tc.model
			cs := connectClient(t, env.srv, memory, tc.client)
			door := openSessionWhole(t, cs)
			var capture mcpserver.ServeResult
			call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture"}, &capture)
			followWaitingLanes(t, cs, door.Session, capture)
			var shown mcpserver.ShowResult
			call(t, cs, "show", map[string]any{"session": door.Session, "ids": []string{fixtureGapID}}, &shown)
			var info mcpserver.InfoResult
			call(t, cs, "info", map[string]any{"session": door.Session}, &info)
			var again mcpserver.ServeResult
			call(t, cs, "next", map[string]any{"session": door.Session, "instance": capture.Instance}, &again)
			var resumed mcpserver.ResumeSessionResult
			call(t, cs, "resume_session", map[string]any{"session": door.Session}, &resumed)

			lined, unlined := memory.results()
			if tc.holdNext && len(unlined) > 0 {
				t.Errorf("every result an Opus or Fable agent gets should carry the hold-next line, none on %v", unlined)
			}
			if !tc.holdNext && len(lined) > 0 {
				t.Errorf("no result an agent on another model gets should carry the hold-next line, got one on %v", lined)
			}
			if !strings.Contains(again.Instructions, "you hold the step") {
				t.Errorf("the step served again should point at the lanes the agent proved, got %.300q", again.Instructions)
			}
		})
	}
}

// An agent listing exactly the tags the last result named gets the step it
// stands at as pointers when the step serves again, also after a read in
// between that served no step.
func TestAgentListingTheHoldNextTagsGetsPointers(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	memory := newAgentMemory()
	memory.holdNext, memory.model = true, opusModel
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
	memory.raw, memory.model = true, opusModel
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
	memory.raw, memory.model = true, opusModel
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

// The proof guide, at the opening and on resume, makes the hold-next proofs
// the act every call owes: a tag left out serves its whole text into the
// agent's context again, and a word is never guessed.
func TestTheProofGuideMakesASkippedProofACost(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	memory := newAgentMemory()
	memory.model = opusModel
	cs := connectAs(t, env.srv, memory)
	door := openSessionWhole(t, cs)
	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": door.Session}, &resumed)
	for name, text := range map[string]string{"opening": door.Instructions, "resume": resumed.Instructions} {
		text = strings.Join(strings.Fields(text), " ")
		for _, want := range []string{"Send held on every call", "A tag left out serves its whole text into your context again", "Never guess a word"} {
			if !strings.Contains(text, want) {
				t.Errorf("the %s's proof guide should say %q, got:\n%s", name, want, text)
			}
		}
		if strings.Contains(text, "costs only") {
			t.Errorf("the %s's proof guide should not make a skipped proof look free, got:\n%s", name, text)
		}
	}
}

// agentModels are a model the hold-next line serves and one it does not, for
// what agents on both get alike.
var agentModels = []string{opusModel, sonnetModel}

// A held lane's pointer names its tag first and sends the agent to the full
// text for the word, so no word of the pointer reads as the word after the
// tag; every agent gets it.
func TestAHeldLanePointerLeavesNoWordAfterItsTag(t *testing.T) {
	for _, agentModel := range agentModels {
		t.Run(agentModel, func(t *testing.T) { testHeldLanePointer(t, agentModel) })
	}
}

func testHeldLanePointer(t *testing.T, agentModel string) {
	env := newTestServer(t, nil, writeLaneStepGraph(t, stepLane{"first", 2}, stepLane{"second", 2}), "")
	memory := newAgentMemory()
	memory.raw, memory.model = true, agentModel
	cs := connectAs(t, env.srv, memory)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "wide-step", "held": []any{}}, &serve)
	proofs := model.ReadAnchors(serve.Instructions)
	if len(proofs) != 2 {
		t.Fatalf("each lane served in full should carry one tag, got %v in:\n%s", proofs, serve.Instructions)
	}
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "held": []any{proofs[0].String()}}, &serve)
	want := "(held: tag " + proofs[0].Tag + " for lane first — prove a tag with the word after it in the full text you were served, never a word of this line;"
	if !strings.Contains(serve.Instructions, want) {
		t.Errorf("the pointer should name the tag first and send the agent to the full text, want %q in:\n%s", want, serve.Instructions)
	}
}

// A held proof that fails, by a wrong word or a tag never issued, gets a
// notice naming the tag beside the full text, so the agent corrects or drops
// it; a right proof gets none. Every agent gets the notice.
func TestAFailedProofIsNamedBesideTheFullText(t *testing.T) {
	for _, agentModel := range agentModels {
		t.Run(agentModel, func(t *testing.T) { testFailedProofNotice(t, agentModel) })
	}
}

func testFailedProofNotice(t *testing.T, agentModel string) {
	env := newTestServer(t, nil, writeLaneStepGraph(t, stepLane{"first", 2}), "")
	memory := newAgentMemory()
	memory.raw, memory.model = true, agentModel
	cs := connectAs(t, env.srv, memory)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "wide-step", "held": []any{}}, &serve)
	lane := proofOf(t, serve.Instructions, "lane first")

	wrong := model.Proof{Tag: lane.Tag, Word: "where"}
	unknown := model.Proof{Tag: "zz00", Word: "word"}
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "held": []any{wrong.String(), unknown.String()}}, &serve)
	if !strings.Contains(serve.Instructions, "Lane first holds a line of its guidance.") {
		t.Errorf("the lane whose proof failed should serve in full, got %.300q", serve.Instructions)
	}
	for _, want := range []string{wrong.String() + " (not the word after that tag)", unknown.String() + " (no such tag)"} {
		if !strings.Contains(serve.ProofNotice, want) {
			t.Errorf("the proof notice should name %q, got %q", want, serve.ProofNotice)
		}
	}

	right := proofOf(t, serve.Instructions, "lane first")
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "held": []any{right.String()}}, &serve)
	if serve.ProofNotice != "" {
		t.Errorf("a right proof should get no notice, got %q", serve.ProofNotice)
	}
}

// At guideReview the line names the playback lanes, as its revise option
// leads back to playback through assemble; an agent proving them then gets
// playback's lanes as pointers.
func TestTheLineAtGuideReviewNamesThePlaybackLanes(t *testing.T) {
	world := proctest.NewWorld(t)
	world.LLM.GuideFindings = []proctest.GuideFinding{{
		Reasoning: "the body leans on 'something' without naming it, so a reader outside the dialogue cannot act",
		Axis:      "stranding", Quote: "something", Repair: "write-in", Severity: "substantive",
	}}
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	memory := newAgentMemory()
	memory.raw, memory.holdNext, memory.model = true, true, opusModel
	cs := connectAs(t, srv, memory)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	followWaitingLanes(t, cs, session, serve)
	draft := map[string]any{
		"body": "A tactical gap: the fixture observes something.", "entryKind": "gap", "layer": "tactical",
		"topics": []string{"testing/fixture"}, "confidence": "medium", "widenReport": "searched from three angles",
	}
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "report": draft}, &serve)
	if serve.Step != "playback" {
		t.Fatalf("the draft should reach playback, got %q: %.300q", serve.Step, serve.Instructions)
	}
	playback := model.ReadAnchors(followWaitingLanes(t, cs, session, serve))
	if len(playback) == 0 {
		t.Fatal("playback should serve its lanes with tags")
	}

	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "yes"}}, &serve)
	if serve.Step != "guideReview" {
		t.Fatalf("the confirm should reach guideReview, got %q: %.300q", serve.Step, serve.Instructions)
	}
	for _, proof := range playback {
		if !named(serve.HoldNext, proof.Tag) {
			t.Errorf("the line at guideReview should name playback's tag %s, got %q", proof.Tag, serve.HoldNext)
		}
	}

	revision := map[string]any{"chooser": "guideReview", "choice": "revise", "fields": map[string]any{"body": "A tactical gap: the fixture observes the missing title."}}
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "report": revision}, &serve)
	if serve.Step != "playback" {
		t.Fatalf("the revision should return to playback, got %q: %.300q", serve.Step, serve.Instructions)
	}
	if !slices.ContainsFunc(playback, func(proof model.Proof) bool {
		return strings.Contains(serve.Instructions, "tag "+proof.Tag+" for lane playback")
	}) {
		t.Errorf("playback served again should point at the lane the agent proved, got %.600q", serve.Instructions)
	}
}
