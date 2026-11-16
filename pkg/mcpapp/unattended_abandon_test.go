package mcpapp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/engine"
	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

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

// Ending a session by its handle is the agent's door too, so it refuses as
// abandoning the move does while a move waits at an unattended step.
func TestAbandonSessionByHandleRefusesABlockedAutonomousCapture(t *testing.T) {
	cases := []struct {
		name       string
		step       string
		autonomous bool
		refused    bool
	}{
		{name: "autonomous at an unattended step", step: "reviseOrOverride", autonomous: true, refused: true},
		{name: "guided at the same step", step: "reviseOrOverride"},
		{name: "autonomous at a step without the bound", step: "playback", autonomous: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			graphDir := filepath.Join(t.TempDir(), "graph")
			path := filepath.Join(graphDir, "2026/06/01-100000-s-tac-aaa.md")
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(gapEntry), 0644); err != nil {
				t.Fatal(err)
			}
			env := newTestServer(t, nil, graphDir, "")
			sessionID := "s_20260706-090000-0b10cced"
			storeCaptureSession(t, env, sessionID, tc.step, tc.autonomous)
			cs := connect(t, env.srv)

			if !tc.refused {
				var down mcpserver.AbandonResult
				call(t, cs, "abandon", map[string]any{"session": sessionID, "reason": "not needed"}, &down)
				if !down.Abandoned {
					t.Fatalf("the session should end by its handle, got %+v", down)
				}
				return
			}
			msg := callExpectError(t, cs, "abandon", map[string]any{"session": sessionID, "reason": "the finding holds"})
			if !strings.Contains(msg, "is not ended by its handle") || !strings.Contains(msg, "i_1 at reviseOrOverride is not abandoned in an autonomous session") {
				t.Fatalf("an agent should not end a session holding a blocked capture, got %q", msg)
			}
			stored, err := env.sessions.Load(t.Context(), sdd.SessionID(sessionID))
			if err != nil {
				t.Fatal(err)
			}
			if stored.Metadata.Ended != nil {
				t.Fatalf("a refused teardown should leave the session open, got %+v", stored.Metadata.Ended)
			}
		})
	}
}
