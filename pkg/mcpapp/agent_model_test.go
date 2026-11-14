package mcpapp_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// An agent that reports Opus on resume_session switches to the hold-next
// guide and line from that result on, and the session keeps the model for
// the calls and resumes that report none.
func TestAResumeReportingOpusSwitchesTheProofGuide(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	memory := newAgentMemory()
	cs := connectClient(t, env.srv, memory, claudeClient)
	door := openSessionWhole(t, cs)
	if !strings.Contains(door.Instructions, everyTagGuide) {
		t.Fatalf("an agent reporting no model should open on the every-tag guide:\n%s", door.Instructions)
	}
	if lined, _ := memory.results(); len(lined) > 0 {
		t.Fatalf("an agent reporting no model should get no hold-next line, got one on %v", lined)
	}

	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": door.Session, "model": opusModel}, &resumed)
	if !strings.Contains(resumed.Instructions, holdNextGuide) || strings.Contains(resumed.Instructions, everyTagGuide) {
		t.Fatalf("the resume reporting Opus should carry the hold-next guide only:\n%s", resumed.Instructions)
	}
	var info mcpserver.InfoResult
	call(t, cs, "info", map[string]any{"session": door.Session}, &info)
	call(t, cs, "resume_session", map[string]any{"session": door.Session}, &resumed)
	if !strings.Contains(resumed.Instructions, holdNextGuide) {
		t.Errorf("a later resume reporting no model should keep the hold-next guide:\n%s", resumed.Instructions)
	}
	lined, _ := memory.results()
	if want := []string{"resume_session", "info", "resume_session"}; !slices.Equal(lined, want) {
		t.Errorf("every result from the resume reporting Opus on should carry the hold-next line, got it on %v", lined)
	}
}

// A delegate runs on the model its dispatch names, so it gets the hold-next
// line without reporting one; a model it reports on resume_session wins over
// the dispatch's. The dispatching agent, reporting none, gets no line.
func TestADelegateRunsOnTheModelItsDispatchNames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verifier sdd.VerifierAgent
		reported string
		holdNext bool
	}{
		{"dispatched opus", sdd.VerifierAgent{Model: "opus"}, "", true},
		{"dispatched sonnet", sdd.VerifierAgent{Model: "sonnet"}, "", false},
		{"reports sonnet over opus", sdd.VerifierAgent{Model: "opus"}, sonnetModel, false},
		{"reports opus over sonnet", sdd.VerifierAgent{Model: "sonnet"}, opusModel, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, child, verify, main := dispatchVerifier(t, tc.verifier)
			delegate := newAgentMemory()
			cs := connectClient(t, srv, delegate, claudeClient)
			resume := map[string]any{"session": child}
			if tc.reported != "" {
				resume["model"] = tc.reported
			}
			var resumed mcpserver.ResumeSessionResult
			call(t, cs, "resume_session", resume, &resumed)
			var page mcpserver.ServeResult
			call(t, cs, "next", map[string]any{"session": child, "instance": verify}, &page)

			lined, unlined := delegate.results()
			if tc.holdNext && (len(unlined) > 0 || !strings.Contains(resumed.Instructions, holdNextGuide)) {
				t.Errorf("the delegate should get the hold-next guide and line on every result, none on %v:\n%s", unlined, resumed.Instructions)
			}
			if !tc.holdNext && (len(lined) > 0 || !strings.Contains(resumed.Instructions, everyTagGuide)) {
				t.Errorf("the delegate should get the every-tag guide and no line, got one on %v:\n%s", lined, resumed.Instructions)
			}
			if lined, _ := main.results(); len(lined) > 0 {
				t.Errorf("the dispatching agent reporting no model should get no hold-next line, got one on %v", lined)
			}
		})
	}
}

// dispatchVerifier drives a capture to the deep verifier's dispatch under a
// Claude client reporting no model, the verifier's subagent configured as
// verifier, and returns the server, the child session and its verify
// instance, and the dispatching agent's memory.
func dispatchVerifier(t *testing.T, verifier sdd.VerifierAgent) (*mcpserver.Server, string, string, *agentMemory) {
	t.Helper()
	entry := &model.Entry{
		ID: "20260601-100000-s-tac-dlg", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Summary: "The delegate fixture observes the verifier's model.", Content: "The delegate fixture observes the verifier's model.",
		Topics: proctest.MustTopics("testing/fixture"), Confidence: "medium",
	}
	world := proctest.NewWorld(t, proctest.WithEntries(entry), proctest.WithVerifier(sdd.VerifierPolicy{Mode: "deep", Claude: verifier}, 200000, 4096))
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	memory := newAgentMemory()
	cs := connectClient(t, srv, memory, claudeClient)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	capture := serve.Instance
	callRaw(t, cs, "gather", map[string]any{"session": session, "instance": capture, "ids": []string{entry.ID}})
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{
		"body": "A tactical gap: the delegate fixture observes the verifier's model.", "entryKind": "gap", "layer": "tactical",
		"refs":   []map[string]any{{"id": entry.ID, "kind": "related", "desc": "the entry this gap sits beside"}},
		"topics": []string{"testing/fixture"}, "confidence": "medium", "widenReport": "opened the fixture entry",
	}}, &serve)
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"}}, &serve)
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "deepDispatch", "choice": "delegate"}}, &serve)
	if len(serve.Dispatches) != 1 {
		t.Fatalf("the dispatching answer should owe one dispatch, got %+v", serve.Dispatches)
	}
	dispatch := serve.Dispatches[0]
	if dispatch.Model != verifier.Model {
		t.Fatalf("the dispatch should name the configured model, got %q", dispatch.Model)
	}
	return srv, dispatch.Session, dispatch.Instance, memory
}
