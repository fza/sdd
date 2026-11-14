package mcpapp_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// listedTag reads the tag an unproved list or a pointer names a piece under:
// bare, after the piece's name (20261113-121039-d-tac-ca2).
var listedTag = regexp.MustCompile(`\(tag ([0-9a-z]{4})\)`)

// listedNames reads the piece names an unproved list carries, newest first.
func listedNames(unproved string) []string {
	_, items, _ := strings.Cut(unproved, ": ")
	var names []string
	for item := range strings.SplitSeq(items, "; ") {
		if name, _, ok := strings.Cut(item, " (tag "); ok {
			names = append(names, name)
		}
	}
	return names
}

func proofOf(t *testing.T, text, id string) model.Proof {
	t.Helper()
	proofs := model.ReadAnchors(text)
	if len(proofs) != 1 {
		t.Fatalf("the %s served in full should carry one tag:\n%s", id, text)
	}
	return proofs[0]
}

// Every result lists the pieces earlier results served in full that the
// agent has not proved since, each by name and bare tag, and the instruction
// to prove them; a proved piece leaves the list, a piece served in full again
// leaves it in that result, and a wrong proof still gets the full text.
func TestResultsListUnprovedPieces(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	memory := newAgentMemory()
	memory.raw = true
	cs := connectAs(t, env.srv, memory)
	opening := openSession(t, cs)
	session := opening.Session
	if opening.Unproved != "" {
		t.Errorf("the opening follows no earlier result, so it should list nothing, got %q", opening.Unproved)
	}

	var shown mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": []any{}}, &shown)
	names := listedNames(shown.Unproved)
	if len(names) == 0 || !strings.Contains(shown.Unproved, "held") {
		t.Fatalf("a call proving nothing should list the opening's pieces with the instruction to prove them, got %q", shown.Unproved)
	}
	if strings.Contains(shown.Unproved, "⟦") || len(listedTag.FindAllString(shown.Unproved, -1)) != len(names) {
		t.Errorf("the list should name each piece by its bare tag, got %q", shown.Unproved)
	}
	for _, name := range names {
		if name == fixtureGapID {
			t.Errorf("the entry this result serves in full should not be listed in it, got %q", shown.Unproved)
		}
	}
	entryProof := proofOf(t, shown.Entries, fixtureGapID)

	var proved mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": []any{entryProof.String()}}, &proved)
	if !strings.Contains(proved.Entries, "(held as tag "+entryProof.Tag+")") || strings.Contains(proved.Entries, "⟦") {
		t.Fatalf("a proved entry should serve its pointer naming its bare tag:\n%s", proved.Entries)
	}
	if !equalNames(listedNames(proved.Unproved), names) {
		t.Errorf("proving the entry should leave the opening's pieces listed as before, got %q", proved.Unproved)
	}

	wrong := model.Proof{Tag: entryProof.Tag, Word: "elsewhere"}
	var guessed mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": []any{wrong.String()}}, &guessed)
	if !strings.Contains(guessed.Entries, "# body") {
		t.Fatalf("a wrong proof should get the entry in full:\n%s", guessed.Entries)
	}
	var info mcpserver.InfoResult
	call(t, cs, "info", map[string]any{"session": session}, &info)
	if after := listedNames(info.Unproved); len(after) == 0 || after[0] != fixtureGapID {
		t.Errorf("the entry served in full again should head the next list, got %q", info.Unproved)
	}

	var all []any
	for _, proof := range model.ReadAnchors(opening.Instructions + opening.Framing + opening.Vocabulary + guessed.Entries) {
		all = append(all, proof.String())
	}
	var cleared mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": all}, &cleared)
	call(t, cs, "info", map[string]any{"session": session}, &info)
	if listedNames(cleared.Unproved) != nil || info.Unproved != "" {
		t.Errorf("every piece proved once should leave the list for good, got %q, then %q", cleared.Unproved, info.Unproved)
	}
}

// A gather's brief lines always serve in full, so no entry they name is
// listed; an entry gather opens in full is.
func TestUnprovedLeavesOutGatherBriefLines(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var reading mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": session, "held": []any{}}, &reading)
	if !strings.Contains(reading.Serve, fixtureGapID+" (") {
		t.Fatalf("the gather should serve the fixture entry as a brief line:\n%s", reading.Serve)
	}
	var info mcpserver.InfoResult
	call(t, cs, "info", map[string]any{"session": session}, &info)
	for _, name := range listedNames(info.Unproved) {
		if strings.HasPrefix(name, "2026") {
			t.Errorf("an entry served as a brief line should not be listed, got %q", info.Unproved)
		}
	}

	var opened mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": []any{}}, &opened)
	call(t, cs, "info", map[string]any{"session": session}, &info)
	if names := listedNames(info.Unproved); len(names) == 0 || names[0] != fixtureGapID {
		t.Errorf("an entry gather opened in full should be listed, got %q", info.Unproved)
	}
}

func equalNames(a, b []string) bool { return strings.Join(a, "\n") == strings.Join(b, "\n") }
