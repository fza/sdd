package mcpapp_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// listedTag reads the tag an unproved list names a piece under: bare, before
// the piece's name.
var listedTag = regexp.MustCompile(`^tag ([0-9a-z]{4}) for `)

// listedNames reads the piece names an unproved list carries, newest first.
func listedNames(unproved string) []string {
	_, items, _ := strings.Cut(unproved, "you still see in held: ")
	var names []string
	for item := range strings.SplitSeq(items, "; ") {
		if tagged := listedTag.FindStringIndex(item); tagged != nil {
			names = append(names, item[tagged[1]:])
		}
	}
	return names
}

// A result that serves a piece lists the pieces earlier results served in
// full that the agent has not proved since, each by bare tag and name, and
// the instruction to prove them; entries are never listed, a result serving
// no piece carries no list, a proved piece leaves the list and a wrong proof
// still gets the full text.
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

	var info mcpserver.InfoResult
	call(t, cs, "info", map[string]any{"session": session}, &info)
	if info.Unproved != "" {
		t.Errorf("a result serving no piece should carry no list, got %q", info.Unproved)
	}

	var shown mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": []any{}}, &shown)
	names := listedNames(shown.Unproved)
	if len(names) == 0 || !strings.Contains(shown.Unproved, "held") {
		t.Fatalf("a call serving an entry and proving nothing should list the opening's pieces with the instruction to prove them, got %q", shown.Unproved)
	}
	_, items, _ := strings.Cut(shown.Unproved, "you still see in held: ")
	if strings.Contains(shown.Unproved, "⟦") || strings.Count(items, "; ")+1 != len(names) {
		t.Errorf("the list should name each piece by its bare tag, got %q", shown.Unproved)
	}
	entryProof := proofOf(t, shown.Entries, fixtureGapID)

	wrong := model.Proof{Tag: entryProof.Tag, Word: "elsewhere"}
	var guessed mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": []any{wrong.String()}}, &guessed)
	if !strings.Contains(guessed.Entries, "# body") {
		t.Fatalf("a wrong proof should get the entry in full:\n%s", guessed.Entries)
	}
	var other mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{principlesFactID}, "held": []any{}}, &other)
	if names := listedNames(other.Unproved); len(names) == 0 || slices.Contains(names, fixtureGapID) {
		t.Errorf("an entry served in full before should never be listed, got %q", other.Unproved)
	}

	var all []any
	for _, proof := range model.ReadAnchors(opening.Instructions + opening.Framing + opening.Vocabulary + guessed.Entries) {
		all = append(all, proof.String())
	}
	var cleared mcpserver.ShowResult
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": all}, &cleared)
	if listedNames(cleared.Unproved) != nil {
		t.Errorf("every piece proved once should leave the list, got %q", cleared.Unproved)
	}
	call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": []any{}}, &cleared)
	if listedNames(cleared.Unproved) != nil {
		t.Errorf("a piece proved once should leave the list for good, got %q", cleared.Unproved)
	}
}
