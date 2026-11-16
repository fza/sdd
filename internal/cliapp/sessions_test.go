package cliapp_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/engine"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// writeCaptureSession stores a session whose capture stands at step, parked
// there since parkedAt unless that is zero.
func writeCaptureSession(t *testing.T, f *appFixture, id, step string, parkedAt time.Time) {
	t.Helper()
	writeCaptureSessionIn(t, f, id, step, parkedAt, "")
}

// writeCaptureSessionIn is writeCaptureSession for a session opened in
// dialogue mode, empty for guided.
func writeCaptureSessionIn(t *testing.T, f *appFixture, id, step string, parkedAt time.Time, dialogue string) {
	t.Helper()
	sessions, err := localadapter.NewFilesystemSessionStore(localadapter.StoreLocation{
		Name: "test", Subject: "local", Project: "example.test/cli",
		Sessions:    filepath.Join(f.root, "state", "sdd", "sessions", "example.test", "cli"),
		StagedBlobs: filepath.Join(f.root, "state", "sdd", "staged-blobs", "example.test", "cli"),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Round(0)
	created, err := sessions.Create(t.Context(), sdd.SessionMetadata{ID: sdd.SessionID(id), Subject: "local", Project: "example.test/cli", Participant: "Local Author", UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	events := []engine.Event{
		{Event: engine.EventSessionMeta, Data: map[string]any{"participant": "Local Author", "dialogue": dialogue}},
		{Instance: "i1", Event: engine.EventStarted, Data: map[string]any{"procedure": "capture", "step": "assemble"}},
		{Instance: "i1", Event: engine.EventTransition, Data: map[string]any{"to": step}},
	}
	if !parkedAt.IsZero() {
		events = append(events, engine.Event{TS: parkedAt, Instance: "i1", Event: engine.EventParked, Data: map[string]any{"step": step, "reason": "blocked again after 1 unattended revise", "held": true}})
	}
	stored := make([]sdd.StoredEvent, 0, len(events))
	for i, event := range events {
		event.V, event.Session, event.Seq = engine.LogVersion, id, i+1
		if event.TS.IsZero() {
			event.TS = now
		}
		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		stored = append(stored, sdd.StoredEvent{CodecVersion: sdd.SessionCodecVersion, Code: sdd.WorkflowEventCode, Payload: payload})
	}
	if _, err := sessions.Append(t.Context(), sdd.SessionID(id), created.Version, sdd.SessionAppend{Events: stored}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsListsAParkedCaptureWithItsStep(t *testing.T) {
	f := newAppFixture(t)
	writeCaptureSession(t, f, "s_parked", "reviseOrOverride", time.Now().Add(-2*time.Hour))
	writeCaptureSession(t, f, "s_open", "playback", time.Time{})

	r := f.run(t, "sessions")
	if r.err != nil {
		t.Fatalf("sessions: %v\n%s", r.err, r.stderr)
	}
	for _, want := range []string{"- i1: capture parked at reviseOrOverride · 2h ago · blocked again after 1 unattended revise", "- i1: capture at playback"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("the listing should show each open move and where it is parked, missing %q in:\n%s", want, r.stdout)
		}
	}
}

func TestSessionsJSONListsEveryOpenSession(t *testing.T) {
	f := newAppFixture(t)
	parkedAt := time.Now().Add(-2 * time.Hour).UTC().Round(time.Second)
	writeCaptureSession(t, f, "s_parked", "reviseOrOverride", parkedAt)
	writeCaptureSession(t, f, "s_open", "playback", time.Time{})

	r := f.run(t, "sessions", "--format", "json")
	if r.err != nil {
		t.Fatalf("sessions --format json: %v\n%s", r.err, r.stderr)
	}
	var listed []struct {
		Session, Procedure, Step, Reason string
		Parked                           bool
		Since                            time.Time
	}
	if err := json.Unmarshal([]byte(r.stdout), &listed); err != nil {
		t.Fatalf("the listing should be JSON: %v\n%s", err, r.stdout)
	}
	bySession := map[string]int{}
	for i, item := range listed {
		bySession[item.Session] = i
	}
	parked, open := listed[bySession["s_parked"]], listed[bySession["s_open"]]
	if len(listed) != 2 || !parked.Parked || parked.Step != "reviseOrOverride" || parked.Procedure != "capture" || parked.Reason == "" || !parked.Since.Equal(parkedAt) {
		t.Errorf("the parked capture should list its step, reason and since, got %+v", listed)
	}
	if open.Parked || open.Step != "playback" || open.Since.IsZero() {
		t.Errorf("an open capture should list as not parked with its last activity, got %+v", open)
	}
}

// sdd sessions abandon is the person's door: it ends an autonomous session
// whose capture waits at an unattended step, which the agent's abandon may
// not drop.
func TestSessionsAbandonEndsAnAutonomousSessionWithABlockedCapture(t *testing.T) {
	f := newAppFixture(t)
	writeCaptureSessionIn(t, f, "s_blocked", "reviseOrOverride", time.Time{}, "auto")
	result := f.run(t, "sessions", "abandon", "s_blocked")
	if result.err != nil {
		t.Fatalf("sdd sessions abandon should end the session, got %v\n%s", result.err, result.stderr)
	}
	if !strings.Contains(result.stdout, "Abandoned s_blocked") {
		t.Fatalf("the abandon should report the session, got %q", result.stdout)
	}
}
