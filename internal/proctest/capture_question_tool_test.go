package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
)

// The playback puts the draft to the person through the question tool of the
// client the session last attached through.
func TestCapture_ThePlaybackNamesTheClientsQuestionTool(t *testing.T) {
	for _, mode := range []proctest.Option{nil, proctest.WithAssistedDialogue()} {
		var options []proctest.Option
		if mode != nil {
			options = append(options, mode)
		}
		world, session := authorshipWorld(t, options...)
		serve := session.Start(t, "capture", nil)
		instance := serve.Instance
		serve = session.Report(t, instance, captureDraft())
		proctest.RequireStep(t, serve, "playback")
		if !strings.Contains(serve.Goal, "AskUserQuestion") {
			t.Errorf("a playback in Claude Code should go through AskUserQuestion, got goal %q", serve.Goal)
		}
		_, resumed := world.Resume(t, session.ID, "codex-mcp-client")
		if served := servedInstance(resumed.Open, instance); served == nil || !strings.Contains(served.Goal, "request_user_input") {
			t.Errorf("a playback resumed in Codex should go through request_user_input, got %+v", served)
		}
	}
}
