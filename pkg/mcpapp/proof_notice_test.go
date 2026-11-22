package mcpapp_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/testsupport"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// A held proof that fails, by a wrong word or a tag never issued, gets a
// notice naming the tag beside the full text, so the agent corrects or drops
// it; a right proof gets none.
func TestAFailedProofIsNamedBesideTheFullText(t *testing.T) {
	env := newTestServer(t, nil, writeLaneStepGraph(t, stepLane{"first", 2}), "")
	memory := newAgentMemory()
	memory.raw = true
	cs := connectAs(t, env.srv, memory)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResponse
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "wide-step", "held": []any{}}, &serve)
	lane := proofOf(t, serve.Instructions, "lane first")

	wrong := model.Proof{Tag: lane.Tag, Word: "where"}
	unknown := model.Proof{Tag: "zz00", Word: "word"}
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "held": []any{wrong.String(), unknown.String()}}, &serve)
	if !strings.Contains(testsupport.Untagged(serve.Instructions), "Lane first holds a line of its guidance.") {
		t.Errorf("the lane whose proof failed should serve in full, got %.300q", serve.Instructions)
	}
	if serve.Notices.Proof == "" {
		t.Fatal("a failed proof should get a proof notice")
	}
	for _, want := range []string{wrong.String() + " (not the word its proof asks for)", unknown.String() + " (no such tag)"} {
		if !strings.Contains(serve.Notices.Proof, want) {
			t.Errorf("the proof notice should name %q, got %q", want, serve.Notices.Proof)
		}
	}

	right := proofOf(t, serve.Instructions, "lane first")
	var proved mcpserver.ServeResponse
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "held": []any{right.String()}}, &proved)
	if proved.Notices.Proof != "" {
		t.Errorf("a right proof should get no notice, got %+v", proved.Notices)
	}
}
