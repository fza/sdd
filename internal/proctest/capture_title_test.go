package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
)

// A capture writes the title from the summary call, the summary check shows
// it, and a correction may replace the title alone.
func TestCapture_TitleIsWrittenShownAndCorrectable(t *testing.T) {
	cases := []struct {
		name        string
		corrections map[string]any
		wantTitle   string
		wantSummary string
	}{
		{name: "title alone", corrections: map[string]any{"correctedTitle": "corrected title"}, wantTitle: "corrected title", wantSummary: "A generated summary."},
		{name: "summary alone", corrections: map[string]any{"correctedSummary": "The corrected summary."}, wantTitle: "generated title", wantSummary: "The corrected summary."},
		{name: "both", corrections: map[string]any{"correctedTitle": "corrected title", "correctedSummary": "The corrected summary."}, wantTitle: "corrected title", wantSummary: "The corrected summary."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			world, session := newCaptureWorld(t, "title-"+strings.ReplaceAll(tc.name, " ", "-"))
			before := entryIDsOnDisk(t, world.GraphDir)
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			session.Report(t, instance, captureDraft())
			serve = session.Answer(t, instance, "playback", "confirm", nil, "publish the observation")
			proctest.RequireStep(t, serve, "verifySummary")
			if !strings.Contains(serve.Instructions, "Generated title: generated title") {
				t.Errorf("the summary check should show the generated title:\n%s", serve.Instructions)
			}
			entryID := writtenEntryID(t, world.GraphDir, before)
			if got := proctest.LoadEntry(t, world.GraphDir, entryID).Title; got != "generated title" {
				t.Fatalf("the capture should write the title from the summary call, got %q", got)
			}

			serve = session.Answer(t, instance, "verifySummary", "drifted", tc.corrections, "")
			proctest.RequireStatus(t, serve, "completed")
			entry := proctest.LoadEntry(t, world.GraphDir, entryID)
			if entry.Title != tc.wantTitle || entry.Summary != tc.wantSummary {
				t.Errorf("each correction should replace only its own field, got title %q summary %q", entry.Title, entry.Summary)
			}
		})
	}
}

func TestCapture_DriftedWithoutACorrectionIsRefused(t *testing.T) {
	world, session := newCaptureWorld(t, "title-no-correction")
	before := entryIDsOnDisk(t, world.GraphDir)
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	session.Answer(t, instance, "playback", "confirm", nil, "publish the observation")
	entryID := writtenEntryID(t, world.GraphDir, before)

	if _, err := session.AnswerErr(t, instance, "verifySummary", "drifted", nil, ""); err == nil || !strings.Contains(err.Error(), "correctedTitle") {
		t.Fatalf("a drifted answer carrying no correction should be refused naming the fields, got %v", err)
	}
	entry := proctest.LoadEntry(t, world.GraphDir, entryID)
	if entry.Title != "generated title" || entry.Summary != "A generated summary." {
		t.Errorf("a refused correction should leave the entry as written, got title %q summary %q", entry.Title, entry.Summary)
	}
}
