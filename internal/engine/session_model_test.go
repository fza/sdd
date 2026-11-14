package engine_test

import (
	"bytes"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
)

// The session keeps the model its agent runs on across a restart: the last
// one recorded wins, and recording the model it holds logs nothing.
func TestSessionModelSurvivesReplay(t *testing.T) {
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	session.SetModel("opus")
	session.SetModel("claude-sonnet-4-5")
	session.SetModel("claude-sonnet-4-5")

	events, err := engine.ReadEvents(bytes.NewReader(log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var recorded int
	for _, event := range events {
		if event.Event == engine.EventModel {
			recorded++
		}
	}
	if recorded != 2 {
		t.Errorf("each change of model should log one event and a repeat none, got %d", recorded)
	}
	if got := replayPieces(t, &log).Model; got != "claude-sonnet-4-5" {
		t.Errorf("a restarted server should hold the model recorded last, got %q", got)
	}
}
