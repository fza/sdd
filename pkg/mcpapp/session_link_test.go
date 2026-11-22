package mcpapp_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/engine"
	test "github.com/networkteam/sdd/internal/testsupport"
	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// A session opened with link carries the texts the earlier session served:
// after a right answer, a move's report schema the earlier session sent
// comes short. Without the link it comes in full.
func TestStartSessionLinkCarriesTheEarlierSessionsTexts(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	agent := newAgentMemory()
	cs := connectAs(t, env.srv, agent)
	first := openSession(t, cs)
	var capture mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": first.Session, "canonical": "capture"}, &capture)
	requireFullReportSchema(t, "the first session's capture", capture.ReportSchema)

	var linked mcpserver.ServeResult
	call(t, cs, "start_session", map[string]any{"link": first.Session}, &linked)
	if strings.Contains(linked.Instructions, "not used") {
		t.Fatalf("a link to a session with served texts should be used, got %.300q", linked.Instructions)
	}
	_, ask, ok := test.LastQuestion(agent.questions[linked.Session])
	if !ok {
		t.Fatal("the linked session's opening should end with a question")
	}
	held := append(append([]string(nil), agent.texts[first.Session]...), agent.texts[linked.Session]...)
	right, ok := test.AnswerQuestion(ask, held)
	if !ok {
		t.Fatalf("the question should be answerable from the texts both sessions served, got %q", ask)
	}
	var carried mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": linked.Session, "canonical": "capture", "answer": right}, &carried)
	requireStubReportSchema(t, "a capture the linked session carries", carried.ReportSchema)

	fresh := openSession(t, cs)
	var unlinked mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": fresh.Session, "canonical": "capture"}, &unlinked)
	requireFullReportSchema(t, "a capture in a session without a link", unlinked.ReportSchema)
}

// A link to a log stock sdd wrote, holding no served texts, opens the session
// with every text in full and one line saying the link went unused.
func TestStartSessionLinkToAStockLogGoesUnused(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	storeCaptureSession(t, env, "s_stock", "assemble", false)
	cs := connectAs(t, env.srv, newAgentMemory())
	var door mcpserver.ServeResult
	call(t, cs, "start_session", map[string]any{"link": "s_stock"}, &door)
	var notice string
	for line := range strings.SplitSeq(door.Instructions, "\n") {
		if strings.Contains(line, "s_stock") {
			notice = line
		}
	}
	if !strings.Contains(notice, "not used") || !strings.Contains(notice, "no record of served texts") {
		t.Fatalf("the opening should say in one line that the link was not used and why, got %.600q", door.Instructions)
	}
	if !strings.Contains(door.Framing, "## Working principles") {
		t.Errorf("an unused link should open with every text in full, got %.300q", door.Framing)
	}
}

func TestStartSessionRefusesAnUnknownLink(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connectAs(t, env.srv, newAgentMemory())
	msg := callExpectError(t, cs, "start_session", map[string]any{"link": "s_20260101-000000-00000000000000000000000000000000"})
	if !strings.Contains(msg, "unknown or was removed") || !strings.Contains(msg, "start_session works without link") {
		t.Errorf("the refusal should say the session is unknown or removed and that the door works without the link, got %q", msg)
	}
}

// storeCaptureSession stores a session whose capture, the shipped one, stands
// at step; autonomous opens it in the auto dialogue mode.
func storeCaptureSession(t *testing.T, env testEnv, id, step string, autonomous bool) {
	t.Helper()
	at := time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC)
	meta := map[string]any{"participant": "Tester"}
	if autonomous {
		meta["dialogue"] = "auto"
	}
	events := []engine.Event{
		{Event: engine.EventSessionMeta, Data: meta},
		{Instance: "i_1", Event: engine.EventStarted, Data: map[string]any{"procedure": "capture", "step": "assemble"}},
		{Instance: "i_1", Event: engine.EventTransition, Data: map[string]any{"to": step}},
	}
	created, err := env.sessions.Create(t.Context(), sdd.SessionMetadata{ID: sdd.SessionID(id), Subject: "tester", Project: "test", Participant: "Tester", UpdatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	stored := make([]sdd.StoredEvent, 0, len(events))
	for i, event := range events {
		event.V, event.TS, event.Session, event.Seq = engine.LogVersion, at, id, i+1
		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		stored = append(stored, sdd.StoredEvent{CodecVersion: sdd.SessionCodecVersion, Code: sdd.WorkflowEventCode, Payload: payload})
	}
	if _, err := env.sessions.Append(t.Context(), sdd.SessionID(id), created.Version, sdd.SessionAppend{Events: stored}); err != nil {
		t.Fatal(err)
	}
}
