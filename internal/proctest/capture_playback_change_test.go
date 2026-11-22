package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/test"
)

// After an adjust the playback shows the person the change: guided with a
// line of context and the full draft once before the final confirm, assisted
// and auto the change only; each has the full draft on request
// (20261121-233256-d-tac-mm2).
func TestCapture_PlaybackAfterAnAdjustShowsTheChange(t *testing.T) {
	const (
		guided   = "After an adjust, the served delta is what changed: show the change with a line of context around it, name what is unchanged, and offer the full draft on request."
		final    = "when they are ready to confirm and anything changed since they last saw the full draft, show it in full once, the description verbatim, before asking."
		assisted = "After an adjust, show them the change only, and the full draft on request."
		auto     = "Where you show the draft to anyone after an adjust, show the change only, and the full draft on request."
	)
	tests := []struct {
		name     string
		options  []proctest.Option
		want     []string
		unwanted []string
	}{
		{name: "guided", want: []string{guided, final}, unwanted: []string{assisted, auto, "Whenever the body changed since the user last saw it in full"}},
		{name: "assisted", options: []proctest.Option{proctest.WithAssistedDialogue()}, want: []string{assisted}, unwanted: []string{guided, final, "the draft you showed them in full"}},
		{name: "auto", options: []proctest.Option{proctest.WithAutonomousDialogue(), clientActor("playback-change-auto")}, want: []string{auto}, unwanted: []string{guided, final, assisted}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, session := newCaptureWorld(t, "playback-change-"+tt.name, tt.options...)
			serve := session.Start(t, "capture", nil)
			serve = session.Report(t, serve.Instance, captureDraft())
			proctest.RequireStep(t, serve, "playback")
			text := test.Untagged(serve.Instructions)
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("the playback should carry %q:\n%s", want, text)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(text, unwanted) {
					t.Errorf("the playback should not carry %q:\n%s", unwanted, text)
				}
			}
		})
	}
}
