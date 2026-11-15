package mcpapp_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

func proofOf(t *testing.T, text, id string) model.Proof {
	t.Helper()
	proofs := model.ReadAnchors(text)
	if len(proofs) != 1 {
		t.Fatalf("the %s served in full should carry one tag:\n%s", id, text)
	}
	return proofs[0]
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

// A held lane's pointer names its tag first and sends the agent to the full
// text for the word, so no word of the pointer reads as the word after the
// tag.
func TestAHeldLanePointerLeavesNoWordAfterItsTag(t *testing.T) {
	env := newTestServer(t, nil, writeLaneStepGraph(t, stepLane{"first", 2}, stepLane{"second", 2}), "")
	memory := newAgentMemory()
	memory.raw = true
	cs := connectAs(t, env.srv, memory)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "wide-step", "held": []any{}}, &serve)
	proofs := model.ReadAnchors(serve.Instructions)
	if len(proofs) != 2 {
		t.Fatalf("each lane served in full should carry one tag, got %v in:\n%s", proofs, serve.Instructions)
	}
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "held": []any{proofs[0].String()}}, &serve)
	pointer := regexp.MustCompile(`\(held: tag ` + proofs[0].Tag + ` for lane first(?: \(next proof ` + proofs[0].Tag + `:<the word right after "[^"]+">\))? — prove each tag with the word its next proof asks for, else the word after the tag, in the full text you were served, never a word of this line;`)
	if !pointer.MatchString(serve.Instructions) {
		t.Errorf("the pointer should name the tag first, its next proof, and send the agent to the full text, in:\n%s", serve.Instructions)
	}
}

// A held proof that fails, by a wrong word or a tag never issued, gets a
// notice naming the tag beside the full text, so the agent corrects or drops
// it; a right proof gets none.
func TestAFailedProofIsNamedBesideTheFullText(t *testing.T) {
	env := newTestServer(t, nil, writeLaneStepGraph(t, stepLane{"first", 2}), "")
	memory := newAgentMemory()
	memory.raw = true
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
	for _, want := range []string{wrong.String() + " (not the word its proof asks for)", unknown.String() + " (no such tag)"} {
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
