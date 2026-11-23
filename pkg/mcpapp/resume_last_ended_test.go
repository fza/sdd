package mcpapp_test

import (
	"testing"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// A resume names the session's most recent ended move and how it ended, so an
// agent asked for the last move's outcome finds a capture it abandoned beside
// the catch-up still open; with no move ended it names none.
func TestResumeNamesTheLastEndedMove(t *testing.T) {
	tests := []struct {
		name    string
		abandon []string
	}{
		{name: "abandoned capture beside an open catch-up", abandon: []string{"capture"}},
		{name: "the later of two abandoned moves", abandon: []string{"catch-up", "capture"}},
		{name: "nothing ended"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestServer(t, nil, writeYoungProjectGraph(t), "")
			cs := connect(t, env.srv)
			session := openSession(t, cs).Session
			want := ""
			for _, canonical := range tt.abandon {
				var move mcpserver.ServeResult
				call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": canonical}, &move)
				callRaw(t, cs, "abandon", map[string]any{"session": session, "instance": move.Instance, "reason": "not now"})
				want = "Last ended move: " + canonical + " " + move.Instance + " (abandoned)"
			}
			callRaw(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "catch-up"})

			resumed := callRaw(t, connect(t, env.srv), "resume_session", map[string]any{"session": session})
			got, _ := resumed["last_ended_move"].(string)
			if got != want {
				t.Fatalf("the resume should name the last ended move and its outcome: want %q, got %q", want, got)
			}
		})
	}
}
